package login

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func authorizationInstructions(authURL string, manualImportURL string, guideURL string, debug bool) string {
	// 统一组装浏览器授权和手动导入提示文案，尽量贴近原程序实测输出。
	var b strings.Builder
	if strings.TrimSpace(authURL) != "" {
		fmt.Fprintf(&b, "请在浏览器中打开以下链接，完成 dreamina 登录授权：\n%s\n", authURL)
	}
	if strings.TrimSpace(manualImportURL) != "" && strings.TrimSpace(guideURL) != "" {
		b.WriteString("\n如果需要 agent 手动导入登录态：\n")
		fmt.Fprintf(&b, "1. 先打开即梦登录页，若已登录可忽略：\n%s\n", guideURL)
		fmt.Fprintf(&b, "2. 登录后打开：\n%s\n", manualImportURL)
		b.WriteString("3. 复制页面返回的完整 JSON。交给 agent 时请在本地终端粘贴全文，或保存为 .json 文件再发送；在群聊/频道里直接粘贴长 JSON 可能被截断。\n")
		b.WriteString("   dreamina import_login_response --file /path/to/dreamina-login.json\n")
		b.WriteString("   或 cat /path/to/dreamina-login.json | dreamina import_login_response\n")
	}
	b.WriteString("\n当前 login 命令会继续等待浏览器回调或本地登录态导入。")
	return strings.TrimSpace(b.String())
}

func printReuseSuccess(v ...any) {
	_ = firstAny(v...)
}

func printBrowserFallbackInstructions(v ...any) {
	// 自动拉起浏览器失败时，回退输出手动登录指引。
	out := io.Writer(os.Stdout)
	var message string
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			continue
		}
		if s, ok := arg.(string); ok && strings.TrimSpace(s) != "" {
			message = s
		}
	}
	if message != "" {
		_, _ = fmt.Fprintln(out, message)
	}
}

func printDebugAuthorization(v ...any) {
	// 调试模式下输出完整授权说明，并补上原程序的手动导入调试提示。
	out := io.Writer(os.Stdout)
	message := ""
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			continue
		}
		if s, ok := arg.(string); ok && strings.TrimSpace(s) != "" {
			message = s
		}
	}
	if message != "" {
		_, _ = fmt.Fprintln(out, message)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "[debug] 该接口返回 /dreamina/cli/v1/dreamina_cli_login 的完整响应 body。")
	_, _ = fmt.Fprintln(out, "[debug] 请将完整响应交给 agent，再执行 dreamina import_login_response 导入。")
	_, _ = fmt.Fprintln(out, "[debug] 长 JSON 请勿仅在聊天频道粘贴（易截断）；请用文件。")
}

func printLoginSuccess(v ...any) {
	out := io.Writer(os.Stdout)
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
		}
	}
	_, _ = fmt.Fprintln(out, "Dreamina 登录成功，本地登录态已保存。")
}

func printAccountSummary(v ...any) {
	// 登录成功或会话复用成功后，输出用户信息和额度摘要。
	out := io.Writer(os.Stdout)
	var summary *AccountSummary
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			continue
		}
		if s, ok := arg.(*AccountSummary); ok {
			summary = s
		}
	}
	if summary == nil || summary.UserCredit == nil {
		return
	}
	if uid := summaryUID(summary); uid != "" {
		_, _ = fmt.Fprintf(out, "UID: %s\n", uid)
	}
	if benefitType := strings.TrimSpace(summary.UserCredit.BenefitType); benefitType != "" {
		_, _ = fmt.Fprintf(out, "VIP: %s\n", benefitType)
	}
	_, _ = fmt.Fprintf(out, "剩余积分: %d\n", summaryRemainingCredit(summary.UserCredit))
}

func printLoginStateTag(v ...any) {
	out := io.Writer(os.Stdout)
	tag := ""
	for _, arg := range v {
		if writer, ok := arg.(io.Writer); ok {
			out = writer
			continue
		}
		if s, ok := arg.(string); ok {
			tag = strings.TrimSpace(s)
		}
	}
	if tag == "" {
		return
	}
	_, _ = fmt.Fprintf(out, "[DREAMINA:%s]\n", tag)
}

func summaryUID(summary *AccountSummary) string {
	if summary == nil || summary.UserInfo == nil {
		return ""
	}
	if summary.UserInfo.UID > 0 {
		return fmt.Sprintf("%d", summary.UserInfo.UID)
	}
	return strings.TrimSpace(summary.UserInfo.UserID)
}

func summaryRemainingCredit(credit *UserCredit) int {
	if credit == nil {
		return 0
	}
	if credit.TotalCredit > 0 {
		return credit.TotalCredit
	}
	return credit.CreditCount
}
