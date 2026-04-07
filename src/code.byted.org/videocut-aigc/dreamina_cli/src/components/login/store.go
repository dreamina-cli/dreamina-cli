package login

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

type Credential struct {
	// Credential 对应本地 credential.json 的核心持久化字段。
	AuthToken        string `json:"auth_token"`
	AutoTokenMD5Sign string `json:"auto_token_md5_sign"`
	RandomSecretKey  string `json:"random_secret_key"`
	SignKeyPairName  string `json:"sign_key_pair_name"`
}

type SessionPayload struct {
	// SessionPayload 预留给 auth_token 解密后的标准会话结构。
}

type UserInfo struct {
	UID         int64  `json:"uid,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type UserCredit struct {
	CreditCount    int    `json:"credit_count,omitempty"`
	VIPCredit      int    `json:"vip_credit,omitempty"`
	GiftCredit     int    `json:"gift_credit,omitempty"`
	PurchaseCredit int    `json:"purchase_credit,omitempty"`
	TotalCredit    int    `json:"total_credit,omitempty"`
	BenefitType    string `json:"benefit_type,omitempty"`
}

type formattedSessionPayload struct {
	Cookie  string                   `json:"cookie,omitempty"`
	Headers *formattedSessionHeaders `json:"headers,omitempty"`
	UID     any                      `json:"uid,omitempty"`
}

type formattedSessionHeaders struct {
	Accept          string `json:"Accept,omitempty"`
	AcceptLanguage  string `json:"Accept-Language,omitempty"`
	Appvr           string `json:"Appvr,omitempty"`
	DeviceTime      string `json:"Device-Time,omitempty"`
	Lan             string `json:"Lan,omitempty"`
	Pf              string `json:"Pf,omitempty"`
	Priority        string `json:"Priority,omitempty"`
	Referer         string `json:"Referer,omitempty"`
	SecChUa         string `json:"Sec-Ch-Ua,omitempty"`
	SecChUaMobile   string `json:"Sec-Ch-Ua-Mobile,omitempty"`
	SecChUaPlatform string `json:"Sec-Ch-Ua-Platform,omitempty"`
	SecFetchDest    string `json:"Sec-Fetch-Dest,omitempty"`
	SecFetchMode    string `json:"Sec-Fetch-Mode,omitempty"`
	SecFetchSite    string `json:"Sec-Fetch-Site,omitempty"`
	Sign            string `json:"Sign,omitempty"`
	SignVer         string `json:"Sign-Ver,omitempty"`
	UserAgent       string `json:"User-Agent,omitempty"`
}

type Manager struct {
	// Manager 负责本地凭证读写、授权地址拼装和登录状态管理。
	dir            string
	credentialPath string
	loginBaseURL   string

	mu             sync.RWMutex
	loginFailure   error
	loginCompleted bool
}

type importCredentialFields struct {
	AuthToken        string
	AutoTokenMD5Sign string
	RandomSecretKey  string
	SignKeyPairName  string
}

func New(v ...any) (*Manager, error) {
	// New 按配置文件初始化凭证目录、凭证路径和登录基础地址。
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &Manager{
		dir:            cfg.Dir,
		credentialPath: cfg.CredentialPath,
		loginBaseURL:   cfg.LoginURL,
	}, nil
}

func FormatSessionPayload(v any) string {
	// 按原程序可见字段把当前会话整理成适合终端输出的 JSON 文本。
	view := buildFormattedSessionPayload(v)
	body, err := json.Marshal(view)
	if err != nil {
		return fmt.Sprint(v)
	}
	body, err = json.MarshalIndent(view, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(body)
}

func buildFormattedSessionPayload(v any) any {
	body, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return v
	}
	root, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	out := formattedSessionPayload{}
	if cookie := sessionCleanString(root["cookie"]); cookie != "" {
		out.Cookie = cookie
	}
	if headers := buildFormattedSessionHeaders(root["headers"]); headers != nil {
		out.Headers = headers
	}
	if uid, ok := buildFormattedSessionUID(root); ok {
		out.UID = uid
	}
	if out.Cookie == "" && out.Headers == nil && out.UID == nil {
		return payload
	}
	return out
}

func buildFormattedSessionHeaders(v any) *formattedSessionHeaders {
	body, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil || len(root) == 0 {
		return nil
	}
	out := &formattedSessionHeaders{
		Accept:          sessionCleanString(root["Accept"]),
		AcceptLanguage:  sessionCleanString(root["Accept-Language"]),
		Appvr:           sessionCleanString(root["Appvr"]),
		DeviceTime:      sessionCleanString(root["Device-Time"]),
		Lan:             sessionCleanString(root["Lan"]),
		Pf:              sessionCleanString(root["Pf"]),
		Priority:        sessionCleanString(root["Priority"]),
		Referer:         sessionCleanString(root["Referer"]),
		SecChUa:         sessionCleanString(root["Sec-Ch-Ua"]),
		SecChUaMobile:   sessionCleanString(root["Sec-Ch-Ua-Mobile"]),
		SecChUaPlatform: sessionCleanString(root["Sec-Ch-Ua-Platform"]),
		SecFetchDest:    sessionCleanString(root["Sec-Fetch-Dest"]),
		SecFetchMode:    sessionCleanString(root["Sec-Fetch-Mode"]),
		SecFetchSite:    sessionCleanString(root["Sec-Fetch-Site"]),
		Sign:            sessionCleanString(root["Sign"]),
		SignVer:         sessionCleanString(root["Sign-Ver"]),
		UserAgent:       sessionCleanString(root["User-Agent"]),
	}
	if *out == (formattedSessionHeaders{}) {
		return nil
	}
	return out
}

func buildFormattedSessionUID(root map[string]any) (any, bool) {
	for _, key := range []string{"uid", "UID", "user_id", "userId", "UserId", "UserID"} {
		value, exists := root[key]
		if !exists {
			continue
		}
		if uid, ok := normalizeFormattedSessionUID(value); ok {
			return uid, true
		}
	}
	return nil, false
}

func sessionCleanString(v any) string {
	value := strings.TrimSpace(fmt.Sprint(v))
	if value == "" || value == "<nil>" || strings.EqualFold(value, "null") {
		return ""
	}
	return value
}

func normalizeFormattedSessionUID(v any) (any, bool) {
	switch value := v.(type) {
	case json.Number:
		text := strings.TrimSpace(value.String())
		if text != "" {
			return json.Number(text), true
		}
	case float64:
		if value == float64(int64(value)) {
			return json.Number(strconv.FormatInt(int64(value), 10)), true
		}
	case int:
		return json.Number(strconv.Itoa(value)), true
	case int64:
		return json.Number(strconv.FormatInt(value, 10)), true
	case string:
		value = strings.TrimSpace(value)
		if value != "" && strings.Trim(value, "0123456789") == "" {
			return json.Number(value), true
		}
	}
	return nil, false
}

func (m *Manager) ResetLoginState() error {
	// 重置登录失败状态和完成标记。
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loginFailure = nil
	m.loginCompleted = false
	return nil
}

func (m *Manager) LastLoginFailure() (any, error) {
	// 返回最近一次登录失败信息。
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.loginFailure == nil {
		return nil, nil
	}
	return m.loginFailure, nil
}

func (m *Manager) markLoginCompleted() error {
	// 标记登录成功，并清理残留失败状态。
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loginFailure = nil
	m.loginCompleted = true
	return nil
}

func (m *Manager) LoginCompleted() (bool, error) {
	// 读取当前登录是否已经完成。
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loginCompleted, nil
}

func (m *Manager) AuthorizationURL(v ...any) (string, error) {
	// 生成普通浏览器登录使用的授权地址，并把本地回调地址带入 callback 参数。
	// 原始程序这里不是 passport/web/web_login，也不是 redirect_uri，
	// 而是直接打开 /ai-tool/login?callback=...&from=cli&random_secret_key=...。
	secretKey, err := m.prepareLoginSecretKey()
	if err != nil {
		return "", err
	}
	port := firstInt(v...)
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/dreamina/callback/save_session", port)

	base := strings.TrimSpace(m.loginBaseURL)
	if base == "" {
		base = m.loginOrigin()
	}

	u, err := parseURL(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Del("aid")
	q.Del("redirect_uri")
	q.Set("random_secret_key", secretKey)
	q.Set("callback", callbackURL)
	q.Set("from", "cli")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (m *Manager) HeadlessAuthorizationURL(v ...any) (string, error) {
	// 原始程序的无头登录也围绕同一条 /ai-tool/login 主链路工作，
	// 这里直接返回浏览器登录 URL，交给无头浏览器捕获二维码与登录响应。
	return m.AuthorizationURL(v...)
}

func (m *Manager) ManualImportURL(v ...any) (string, error) {
	// 生成手动导入登录响应时访问的页面地址。
	secretKey, err := m.prepareLoginSecretKey()
	if err != nil {
		return "", err
	}
	u, err := parseURL(m.loginOrigin() + "/dreamina/cli/v1/dreamina_cli_login")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("aid", "513695")
	q.Set("random_secret_key", secretKey)
	q.Set("web_version", "7.5.0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (m *Manager) LoginGuideURL(v ...any) (string, error) {
	// 返回登录引导页地址。
	return m.loginOrigin() + "/ai-tool/login", nil
}

func (m *Manager) AuthorizationInstructions(v ...any) string {
	// 组合浏览器授权和手动导入提示文案。
	authURL := firstString(v...)
	manualURL, _ := m.ManualImportURL()
	guideURL, _ := m.LoginGuideURL()
	return authorizationInstructions(authURL, manualURL, guideURL, false)
}

func (m *Manager) loginOrigin(v ...any) string {
	// 提取登录基础地址的 scheme 和 host；解析失败时回落到默认站点。
	u, err := parseURL(strings.TrimSpace(m.loginBaseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "https://jimeng.jianying.com"
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

func (m *Manager) ParseAuthToken(v ...any) (any, error) {
	// 读取本地可用凭证，并解析其中的 auth_token。
	cred, err := m.loadUsableCredential()
	if err != nil {
		return nil, err
	}
	return ParseAuthToken(cred.AuthToken, cred.RandomSecretKey)
}

func (m *Manager) HasUsableCredential() bool {
	// 判断当前本地是否存在可直接使用的登录凭证。
	_, err := m.loadUsableCredential()
	return err == nil
}

func (m *Manager) RequireUsableCredential() error {
	// 要求当前必须有可用凭证，否则返回面向用户的登录提示。
	if _, err := m.loadUsableCredential(); err != nil {
		return fmt.Errorf("未检测到有效登录态，请先执行 dreamina login")
	}
	return nil
}

func (m *Manager) LoadUsableSession() (any, error) {
	// 优先读取 cookie.json；只有 cookie 会话不可用时，才回退到 credential.json 解析 auth_token。
	if payload, err := m.LoadCookieSession(); err == nil {
		return payload, nil
	}
	return m.ParseAuthToken()
}

func (m *Manager) RequireUsableSession() error {
	// 只要本地存在可用 cookie 会话或 credential 登录态，就视为已经登录。
	if _, err := m.LoadUsableSession(); err != nil {
		return fmt.Errorf("未检测到有效登录态，请先执行 dreamina login")
	}
	return nil
}

func (m *Manager) LoadCookieSession() (any, error) {
	// 从本地 cookie.json 读取 query_result 可直接使用的最小会话视图。
	if m == nil {
		return nil, fmt.Errorf("login manager is not initialized")
	}
	body, err := os.ReadFile(filepath.Join(m.dir, "cookie.json"))
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cookie session: %w", err)
	}
	if cookie := strings.TrimSpace(fmt.Sprint(payload["cookie"])); cookie == "" || cookie == "<nil>" {
		return nil, fmt.Errorf("cookie session is unusable")
	}
	return payload, nil
}

func (m *Manager) RequireUsableCookieSession() error {
	if _, err := m.LoadCookieSession(); err != nil {
		return fmt.Errorf("未检测到有效 cookie 会话，请先准备 ~/.dreamina_cli/cookie.json")
	}
	return nil
}

func (m *Manager) ValidateAuthToken(v ...any) error {
	// 通过加载可用凭证来触发 auth_token 校验流程。
	_, err := m.loadUsableCredential()
	return err
}

func (m *Manager) ImportLoginResponseJSON(v ...any) error {
	// 导入登录响应时会校验 JSON、提取凭证字段、验证 auth_token，
	// 最后落盘并标记登录完成。
	raw := firstBytes(v...)
	if !json.Valid(raw) {
		return fmt.Errorf("request body must be valid json")
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("unmarshal login response: %w", err)
	}

	if failure := buildLoginFailure(payload); failure != nil {
		_ = m.setLoginFailure(failure)
		return failure
	}

	if err := validateLoginResponse(payload); err != nil {
		return err
	}

	fields := importFieldsFromAny(payload)

	cred, err := m.loadCredential()
	if err != nil && !isNotExist(err) {
		return err
	}
	if cred == nil {
		cred = &Credential{}
	}

	secretKey, err := m.resolveImportedSecretKey(cred, fields)
	if err != nil {
		return err
	}
	if secretKey == "" {
		return fmt.Errorf("random_secret_key is missing locally, please rerun dreamina login")
	}

	cred.AuthToken = strings.TrimSpace(fields.AuthToken)
	cred.AutoTokenMD5Sign = strings.TrimSpace(fields.AutoTokenMD5Sign)
	cred.SignKeyPairName = strings.TrimSpace(fields.SignKeyPairName)
	cred.RandomSecretKey = secretKey

	sessionPayload, err := ParseAuthToken(cred.AuthToken, cred.RandomSecretKey)
	if err != nil {
		return fmt.Errorf("invalid auth token: %w", err)
	}
	if !hasUsableSessionPayload(sessionPayload) {
		return fmt.Errorf("invalid auth token: parsed session payload is unusable")
	}

	if err := m.saveCredential(cred); err != nil {
		return fmt.Errorf("save credential: %w", err)
	}

	return m.markLoginCompleted()
}

func (m *Manager) ClearCredential() error {
	// 删除本地凭证文件，并重置登录状态。
	err := os.Remove(m.credentialPath)
	if err != nil && !isNotExist(err) {
		return err
	}
	return m.ResetLoginState()
}

func (m *Manager) prepareLoginSecretKey() (string, error) {
	// 读取或生成 random_secret_key，供授权和手动导入链路复用。
	cred, err := m.loadCredential()
	if err != nil && !isNotExist(err) {
		return "", err
	}
	if cred == nil {
		cred = &Credential{}
	}
	if strings.TrimSpace(cred.RandomSecretKey) != "" {
		return cred.RandomSecretKey, nil
	}

	secretKey, err := randomHex(16)
	if err != nil {
		return "", err
	}
	cred.RandomSecretKey = secretKey
	if err := m.saveCredential(cred); err != nil {
		return "", err
	}
	return secretKey, nil
}

func (m *Manager) loadUsableCredential() (*Credential, error) {
	// 读取凭证后会同时校验签名、解密 token，并确保会话内容可被后续链路使用。
	cred, err := m.loadCredential()
	if err != nil {
		return nil, err
	}
	switch {
	case strings.TrimSpace(cred.AuthToken) == "":
		return nil, fmt.Errorf("auth_token is required")
	case strings.TrimSpace(cred.RandomSecretKey) == "":
		return nil, fmt.Errorf("random_secret_key is missing locally, please rerun dreamina login")
	case strings.TrimSpace(cred.AutoTokenMD5Sign) == "":
		return nil, fmt.Errorf("auto_token_md5_sign is required")
	case strings.TrimSpace(cred.SignKeyPairName) == "":
		return nil, fmt.Errorf("sign_key_pair_name is required")
	}

	if err := verifyAuthTokenSignature(cred.AuthToken, cred.AutoTokenMD5Sign, cred.SignKeyPairName); err != nil {
		return nil, err
	}
	payload, err := ParseAuthToken(cred.AuthToken, cred.RandomSecretKey)
	if err != nil {
		return nil, err
	}
	if !hasUsableSessionPayload(payload) {
		return nil, fmt.Errorf("parsed auth token payload is unusable")
	}
	return cred, nil
}

func (m *Manager) loadCredential() (*Credential, error) {
	// 从磁盘读取 credential.json 并解析成 Credential。
	body, err := os.ReadFile(m.credentialPath)
	if err != nil {
		return nil, err
	}
	var cred Credential
	if err := json.Unmarshal(body, &cred); err != nil {
		return nil, fmt.Errorf("decode credential: %w", err)
	}
	return &cred, nil
}

func (m *Manager) saveCredential(v ...any) error {
	// 以原子写方式把凭证保存到本地文件。
	cred, _ := firstCredential(v...)
	if cred == nil {
		return fmt.Errorf("credential is required")
	}
	body, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	body = append(body, '\n')
	return writeFileAtomically(m.credentialPath, body, 0o600)
}

func randomHex(v ...any) (string, error) {
	size := firstInt(v...)
	if size <= 0 {
		size = 16
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeFileAtomically(v ...any) error {
	// 先写临时文件再原子替换，避免凭证文件写到一半被中断。
	path := firstString(v...)
	body := firstBytes(v...)
	perm := os.FileMode(0o600)
	for _, arg := range v {
		if mode, ok := arg.(os.FileMode); ok {
			perm = mode
		}
	}
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(perm); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func importFieldsFromAny(v any) importCredentialFields {
	return importCredentialFields{
		AuthToken:        strings.TrimSpace(findFirstStringByKeys(v, "auth_token")),
		AutoTokenMD5Sign: strings.TrimSpace(findFirstStringByKeys(v, "auto_token_md5_sign")),
		RandomSecretKey:  strings.TrimSpace(findFirstStringByKeys(v, "random_secret_key")),
		SignKeyPairName:  strings.TrimSpace(findFirstStringByKeys(v, "sign_key_pair_name")),
	}
}

func findFirstStringByKeys(v any, keys ...string) string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[strings.ToLower(key)] = struct{}{}
	}

	var walk func(any) string
	walk = func(node any) string {
		switch value := node.(type) {
		case map[string]any:
			for key, item := range value {
				if _, ok := keySet[strings.ToLower(key)]; ok {
					if s := stringifyScalar(item); strings.TrimSpace(s) != "" {
						return s
					}
				}
			}
			for _, item := range value {
				if s := walk(item); s != "" {
					return s
				}
			}
		case []any:
			for _, item := range value {
				if s := walk(item); s != "" {
					return s
				}
			}
		}
		return ""
	}

	return walk(v)
}

func stringifyScalar(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}

func sanitizeSessionValue(key string, value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for k, v := range current {
			out[k] = sanitizeSessionValue(k, v)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(current))
		for k, v := range current {
			out[k] = sanitizeSessionString(k, v)
		}
		return out
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, sanitizeSessionValue(key, item))
		}
		return out
	case string:
		return sanitizeSessionString(key, current)
	default:
		return value
	}
}

func sanitizeSessionString(key string, value string) string {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	if lowerKey == "cookie" || strings.Contains(lowerKey, "cookie") {
		return summarizeCookieHeader(value)
	}
	if strings.Contains(lowerKey, "auth") ||
		strings.Contains(lowerKey, "token") ||
		strings.Contains(lowerKey, "secret") ||
		strings.Contains(lowerKey, "sign") ||
		strings.Contains(lowerKey, "session") {
		return redactString(value)
	}
	return value
}

func summarizeCookieHeader(value string) string {
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		if idx := strings.Index(part, "="); idx >= 0 {
			name = strings.TrimSpace(part[:idx])
		}
		if name == "" {
			continue
		}
		out = append(out, name+"=<redacted>")
	}
	return strings.Join(out, "; ")
}

func redactString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "<redacted>"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

func firstBytes(v ...any) []byte {
	for _, arg := range v {
		if body, ok := arg.([]byte); ok {
			return body
		}
	}
	return nil
}

func firstString(v ...any) string {
	for _, arg := range v {
		if s, ok := arg.(string); ok {
			return s
		}
	}
	return ""
}

func firstInt(v ...any) int {
	for _, arg := range v {
		if n, ok := arg.(int); ok {
			return n
		}
	}
	return 0
}

func firstCredential(v ...any) (*Credential, bool) {
	for _, arg := range v {
		if cred, ok := arg.(*Credential); ok {
			return cred, true
		}
	}
	return nil, false
}

func (m *Manager) resolveImportedSecretKey(cred *Credential, fields importCredentialFields) (string, error) {
	candidates := []string{
		strings.TrimSpace(cred.RandomSecretKey),
		strings.TrimSpace(fields.RandomSecretKey),
	}
	if strings.TrimSpace(cred.RandomSecretKey) == "" && strings.TrimSpace(fields.RandomSecretKey) == "" {
		secretKey, err := m.prepareLoginSecretKey()
		if err != nil {
			return "", err
		}
		candidates = append(candidates, strings.TrimSpace(secretKey))
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		payload, err := ParseAuthToken(strings.TrimSpace(fields.AuthToken), candidate)
		if err == nil && hasUsableSessionPayload(payload) {
			return candidate, nil
		}
	}
	if text := strings.TrimSpace(fields.RandomSecretKey); text != "" {
		return text, nil
	}
	if text := strings.TrimSpace(cred.RandomSecretKey); text != "" {
		return text, nil
	}
	return "", nil
}

func hasUsableSessionPayload(payload any) bool {
	switch value := payload.(type) {
	case map[string]any:
		if text := strings.TrimSpace(fmt.Sprint(value["cookie"])); text != "" && text != "<nil>" {
			return true
		}
		if headers, ok := value["headers"].(map[string]any); ok && len(headers) > 0 {
			return true
		}
		for _, key := range []string{"uid", "user_id"} {
			if text := strings.TrimSpace(fmt.Sprint(value[key])); text != "" && text != "<nil>" {
				return true
			}
		}
		for _, item := range value {
			if hasUsableSessionPayload(item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if hasUsableSessionPayload(item) {
				return true
			}
		}
	}
	return false
}
