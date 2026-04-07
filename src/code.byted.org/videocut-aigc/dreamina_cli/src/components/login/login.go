package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	"code.byted.org/videocut-aigc/dreamina_cli/config"
	"code.byted.org/videocut-aigc/dreamina_cli/server"
)

type RunOptions struct {
	Headless bool
	Debug    bool
	Port     int
}

type AccountSummary struct {
	UserInfo   *UserInfo
	UserCredit *UserCredit
}

func (s *Service) RunLogin(v ...any) error {
	// RunLogin 走普通登录流程，优先尝试复用本地会话。
	return s.run(append([]any{false}, v...)...)
}

func (s *Service) RunRelogin(v ...any) error {
	// RunRelogin 会先清理本地凭证，再强制重新登录。
	return s.run(append([]any{true}, v...)...)
}

func (s *Service) run(v ...any) error {
	// 重登路径先清空本地凭证；普通登录则先尝试复用已有会话，
	// 只有无法复用时才进入浏览器登录流程。
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}

	relogin, opts, out := parseRunInputs(v...)
	if relogin {
		if err := s.mgr.ClearCredential(); err != nil {
			return err
		}
	} else {
		reused, err := s.tryReuseLoginSession(out)
		if err != nil {
			return err
		}
		if reused {
			return nil
		}
	}
	return s.runBrowserLogin(opts, out)
}

func (s *Service) runBrowserLogin(v ...any) error {
	// 浏览器登录会先启动本地回调服务并生成授权地址，
	// 然后根据模式选择无头登录或本机拉起浏览器，最后等待回调写入凭证。
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}

	opts, out := parseRunOptionsAndWriter(v...)
	if err := s.mgr.ResetLoginState(); err != nil {
		return err
	}
	startSnapshot := fmt.Sprint(s.credentialSnapshot())

	if s.serverStarter == nil {
		s.serverStarter = &defaultServerStarter{}
	}
	localServer, err := s.serverStarter.Start([]server.Route{
		{
			Pattern: "/dreamina/callback/save_session",
			Handler: s.mgr.CallbackHandler(),
		},
	}, opts.Port)
	if err != nil {
		return err
	}
	defer func() {
		_ = localServer.Shutdown(context.Background())
	}()

	var (
		authURL string
	)
	if opts.Headless {
		authURL, err = s.mgr.HeadlessAuthorizationURL(localServer.Port())
	} else {
		authURL, err = s.mgr.AuthorizationURL(localServer.Port())
	}
	if err != nil {
		return err
	}
	manualURL, _ := s.mgr.ManualImportURL()
	guideURL, _ := s.mgr.LoginGuideURL()
	instructions := authorizationInstructions(authURL, manualURL, guideURL, opts.Debug || opts.Headless)

	if opts.Debug {
		printDebugAuthorization(out, instructions)
	}
	printedInstructions := opts.Debug

	if opts.Headless {
		if s.headlessRunner == nil {
			s.headlessRunner = &headlessBrowserRunner{}
		}
		loginCtx, cancelLogin := context.WithCancel(context.Background())
		defer cancelLogin()
		if err := s.headlessRunner.Start(loginCtx, authURL); err != nil {
			_, _ = fmt.Fprintln(out, err.Error())
			printBrowserFallbackInstructions(out, instructions)
			printedInstructions = true
		} else {
			_, _ = fmt.Fprintln(out, "已启动无头浏览器，二维码会在下方显示；扫码后请在手机上确认授权。")
		}
	} else {
		if s.browserOpener == nil {
			s.browserOpener = BrowserOpenerFunc(openBrowser)
		}
		if err := s.browserOpener.Open(authURL); err != nil {
			_, _ = fmt.Fprintf(out, "自动打开浏览器失败，请手动继续登录：%s\n", err.Error())
			printBrowserFallbackInstructions(out, instructions)
			printedInstructions = true
		} else if !opts.Debug {
			_, _ = fmt.Fprintln(out, "已尝试打开默认浏览器，请在页面中完成 Dreamina 登录授权。")
		}
	}
	if opts.Debug && !printedInstructions {
		printBrowserFallbackInstructions(out, instructions)
	}
	if err := s.waitForLogin(context.Background(), 2*time.Minute, startSnapshot); err != nil {
		return err
	}
	summary, _ := s.fetchAccountSummary(out)
	printLoginSuccess(out, summary)
	printAccountSummary(out, summary)
	printLoginStateTag(out, "LOGIN_SUCCESS")
	return nil
}

func (s *Service) waitForLogin(v ...any) error {
	// 等待期间会持续检查登录失败状态、完成标记和凭证快照变化，
	// 任一条件满足即返回成功或错误，直到超时。
	ctx := context.Background()
	timeout := 3 * time.Second
	startSnapshot := ""
	for _, arg := range v {
		switch value := arg.(type) {
		case context.Context:
			ctx = value
		case time.Duration:
			timeout = value
		case string:
			startSnapshot = value
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		failure, _ := s.mgr.LastLoginFailure()
		if err, ok := failure.(error); ok && err != nil {
			return err
		}

		completed, _ := s.mgr.LoginCompleted()
		if completed {
			return nil
		}

		nextSnapshot := fmt.Sprint(s.credentialSnapshot())
		if startSnapshot != "" && nextSnapshot != "" && nextSnapshot != startSnapshot {
			return nil
		}
		if _, err := s.mgr.loadUsableCredential(); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("login wait timed out")
}

func (s *Service) credentialSnapshot(v ...any) any {
	// 读取并序列化当前凭证，供登录前后做快照对比。
	cred, err := s.mgr.loadCredential()
	if err != nil {
		return ""
	}
	body, err := json.Marshal(cred)
	if err != nil {
		return ""
	}
	return string(body)
}

func (s *Service) tryReuseLoginSession(v ...any) (bool, error) {
	// 如果本地会话仍可用，就直接输出复用结果并跳过重新登录。
	out := io.Writer(os.Stdout)
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			break
		}
	}
	summary, err := s.fetchAccountSummary(v...)
	if err != nil {
		return false, err
	}
	if summary != nil {
		printReuseSuccess(out)
		printAccountSummary(out, summary)
		printLoginStateTag(out, "LOGIN_REUSED")
		return true, nil
	}
	return false, nil
}

func (s *Service) fetchAccountSummary(v ...any) (*AccountSummary, error) {
	// 账号摘要会先解析当前登录会话，再并发探测用户信息和额度信息。
	payload, err := s.mgr.ParseAuthToken()
	if err != nil {
		return nil, nil
	}
	return fetchAccountSummaryFromPayload(payload, s.accountUserInfoProbe(), s.accountUserCreditProbe())
}

func (s *Service) accountUserInfoProbe() UserInfoProbe {
	if s != nil && s.userInfoProbe != nil {
		return s.userInfoProbe
	}
	client := commerceclient.New()
	return client.GetUserInfo
}

func (s *Service) accountUserCreditProbe() UserCreditProbe {
	if s != nil && s.userCreditProbe != nil {
		return s.userCreditProbe
	}
	client := commerceclient.New()
	return client.GetUserCredit
}

func fetchAccountSummaryFromPayload(payload any, userInfoProbe UserInfoProbe, userCreditProbe UserCreditProbe) (*AccountSummary, error) {
	summary := &AccountSummary{}

	var (
		wg             sync.WaitGroup
		userInfoErr    error
		userCreditErr  error
		commerceInfo   *commerceclient.UserInfo
		commerceCredit *commerceclient.UserCredit
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		if userInfoProbe == nil {
			userInfoErr = fmt.Errorf("user info fetcher is not configured")
			return
		}
		info, err := userInfoProbe(context.Background(), payload)
		if err != nil {
			userInfoErr = err
			return
		}
		commerceInfo = info
	}()
	go func() {
		defer wg.Done()
		if userCreditProbe == nil {
			userCreditErr = fmt.Errorf("user credit fetcher is not configured")
			return
		}
		credit, err := userCreditProbe(context.Background(), payload)
		if err != nil {
			userCreditErr = err
			return
		}
		commerceCredit = credit
	}()
	wg.Wait()

	if commerceInfo != nil {
		summary.UserInfo = &UserInfo{
			UserID:      commerceInfo.UserID,
			DisplayName: commerceInfo.DisplayName,
		}
		if session, ok := payload.(map[string]any); ok {
			if uid, ok := sessionUID(session); ok {
				summary.UserInfo.UID = uid
				if summary.UserInfo.UserID == "" {
					summary.UserInfo.UserID = fmt.Sprintf("%d", uid)
				}
			}
		}
	}
	if summary.UserInfo == nil {
		if session, ok := payload.(map[string]any); ok {
			if uid, ok := sessionUID(session); ok {
				summary.UserInfo = &UserInfo{
					UID:    uid,
					UserID: fmt.Sprintf("%d", uid),
				}
			}
		}
	}
	if commerceCredit != nil {
		summary.UserCredit = &UserCredit{
			CreditCount:    commerceCredit.CreditCount,
			VIPCredit:      commerceCredit.VIPCredit,
			GiftCredit:     commerceCredit.GiftCredit,
			PurchaseCredit: commerceCredit.PurchaseCredit,
			TotalCredit:    commerceCredit.TotalCredit,
			BenefitType:    commerceCredit.BenefitType,
		}
	}
	// 原始行为更接近“部分成功可接受，但两个探针都失败不能伪装成成功摘要”。
	// 因此只有在至少拿到一侧有效信息时，才回补默认的零额度结构体供展示层使用。
	if summary.UserInfo != nil || summary.UserCredit != nil {
		if summary.UserCredit == nil {
			summary.UserCredit = &UserCredit{
				CreditCount:    0,
				VIPCredit:      0,
				GiftCredit:     0,
				PurchaseCredit: 0,
				TotalCredit:    0,
				BenefitType:    "",
			}
		}
		return summary, nil
	}
	return nil, errorsJoin(userInfoErr, userCreditErr)
}

func errorsJoin(v ...any) error {
	parts := make([]string, 0, len(v))
	for _, arg := range v {
		err, ok := arg.(error)
		if !ok || err == nil {
			continue
		}
		parts = append(parts, err.Error())
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(stringsJoin(parts, "; "))
}

func sessionUID(v map[string]any) (int64, bool) {
	return findSessionUID(v)
}

func stringsJoin(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += sep + items[i]
	}
	return out
}

func findSessionUID(node any) (int64, bool) {
	switch value := node.(type) {
	case map[string]any:
		// 账号摘要 fallback 依赖这里把会话中的 UID 提出来。
		// 兼容 UserID/UID 等别名后，即使后端 schema 在大小写或命名上漂移，也不会把已登录用户误判成“无 uid”。
		for _, key := range []string{"uid", "user_id", "UID", "userId", "UserId", "UserID"} {
			raw, ok := value[key]
			if !ok {
				continue
			}
			if uid, ok := parseSessionUIDScalar(raw); ok {
				return uid, true
			}
		}
		for _, item := range value {
			if uid, ok := findSessionUID(item); ok {
				return uid, true
			}
		}
	case []any:
		for _, item := range value {
			if uid, ok := findSessionUID(item); ok {
				return uid, true
			}
		}
	}
	return 0, false
}

func parseSessionUIDScalar(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		return int64(value), true
	case json.Number:
		n, err := value.Int64()
		if err == nil {
			return n, true
		}
	case string:
		var n int64
		for _, ch := range value {
			if ch < '0' || ch > '9' {
				n = 0
				break
			}
			n = n*10 + int64(ch-'0')
		}
		if n > 0 {
			return n, true
		}
	}
	return 0, false
}

func parseRunInputs(v ...any) (bool, RunOptions, io.Writer) {
	relogin := false
	opts := RunOptions{Port: config.DefaultLoginCallbackPort}
	out := io.Writer(os.Stdout)
	for _, arg := range v {
		switch value := arg.(type) {
		case bool:
			relogin = value
		case RunOptions:
			opts = value
		case io.Writer:
			out = value
		}
	}
	if opts.Port <= 0 {
		opts.Port = config.DefaultLoginCallbackPort
	}
	return relogin, opts, out
}

func parseRunOptionsAndWriter(v ...any) (RunOptions, io.Writer) {
	_, opts, out := parseRunInputs(v...)
	return opts, out
}
