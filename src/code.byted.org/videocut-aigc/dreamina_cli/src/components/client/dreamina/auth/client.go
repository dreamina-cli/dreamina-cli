package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

type HTTPClient struct {
	http *httpclient.Client
}

type ValidateResponse struct {
	Valid   bool   `json:"valid"`
	Nonce   string `json:"nonce"`
	LogID   string `json:"log_id"`
	Message string `json:"message"`
	Session any    `json:"session,omitempty"`
	Curl    string `json:"curl,omitempty"`
}

// New 创建认证校验客户端；如果没有外部注入 HTTP 客户端，就使用默认实现。
func New(v ...any) *HTTPClient {
	var http *httpclient.Client
	for _, arg := range v {
		if value, ok := arg.(*httpclient.Client); ok {
			http = value
			break
		}
	}
	if http == nil {
		http, _ = httpclient.New()
	}
	return &HTTPClient{http: http}
}

// ValidateAuthToken 调用远端 token 校验接口，并把返回的 session 信息合并回当前本地会话。
func (c *HTTPClient) ValidateAuthToken(ctx context.Context, session any) (*ValidateResponse, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	nonce, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	headers := collectForwardHeaders(session)
	query := map[string]any{"nonce": nonce}
	req, err := c.http.NewRequest(ctx, "GET", "/auth/v1/token/validate", nil, headers, query)
	if err != nil {
		return nil, err
	}
	c.http.ApplyBackendHeaders(req)
	curl := buildRequestCurl(req)
	respAny, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	body, enc, err := httpclient.ReadDecodedResponseBody(respAny)
	if err != nil {
		return nil, err
	}
	resp, _ := respAny.(*httpclient.Response)
	if resp == nil || resp.StatusCode != 200 {
		return nil, fmt.Errorf("validate auth token failed: status=%d preview=%s curl=%s", responseStatus(resp), formatResponsePreview(body, enc, resp), curl)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("validate auth token failed: status=%d preview=%s curl=%s", responseStatus(resp), formatResponsePreview(body, enc, resp), curl)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("validate auth token failed: status=%d preview=%s curl=%s", responseStatus(resp), formatResponsePreview(body, enc, resp), curl)
	}
	logID := strings.TrimSpace(recursiveStringValue(
		payload,
		"log_id", "logid", "LogID",
		"request_id", "requestId", "RequestId", "RequestID",
		"trace_id", "traceId", "TraceId", "TraceID",
		"rid", "Rid",
	))
	if logID == "" {
		logID = buildFallbackLogID("auth-validate")
	}
	message := strings.TrimSpace(recursiveStringValue(
		payload,
		"message", "msg", "Message",
		"description", "Description",
		"detail", "Detail",
		"error_message", "errorMessage", "ErrorMessage",
		"err_msg", "errMsg", "ErrMsg",
		"ret_msg", "retMsg", "RetMsg",
	))
	if message == "" {
		message = "validation succeeded"
	}
	valid := authResponseSucceeded(payload)
	return &ValidateResponse{
		Valid:   valid,
		Nonce:   nonce,
		LogID:   logID,
		Message: message,
		Session: mergeValidatedSession(session, payload),
		Curl:    curl,
	}, nil
}

func randomHex(n int) (string, error) {
	if n <= 0 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func buildFallbackLogID(seed string) string {
	seed = strings.ToLower(strings.TrimSpace(seed))
	seed = strings.ReplaceAll(seed, " ", "-")
	if seed == "" {
		seed = "request"
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%s-%x", seed, buf)
	}
	return seed
}

func shouldSkipForwardHeader(k string) bool {
	k = strings.ToLower(strings.TrimSpace(k))
	switch k {
	case "", "host", "content-length", "connection":
		return true
	default:
		return false
	}
}

func formatResponsePreview(v ...any) string {
	var (
		body []byte
		enc  string
		resp *httpclient.Response
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case []byte:
			body = value
		case string:
			enc = value
		case *httpclient.Response:
			resp = value
		}
	}
	preview := strings.TrimSpace(string(body))
	if len(preview) > 240 {
		preview = preview[:240] + "..."
	}
	if resp != nil {
		return fmt.Sprintf("status=%d encoding=%s body=%s", resp.StatusCode, responseEncodingLabel(enc), preview)
	}
	return fmt.Sprintf("encoding=%s body=%s", responseEncodingLabel(enc), preview)
}

func responseEncodingLabel(v ...any) string {
	for _, arg := range v {
		if s, ok := arg.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return "identity"
}

func buildRequestCurl(v ...any) string {
	for _, arg := range v {
		req, ok := arg.(*httpclient.Request)
		if !ok || req == nil {
			continue
		}
		parts := []string{"curl", "-X", req.Method, quoteShell(req.Path)}
		keys := sortedMapKeys(req.Headers)
		for _, key := range keys {
			parts = append(parts, "-H", quoteShell(key+": "+redactHeaderValue(key, req.Headers[key])))
		}
		if len(req.Body) > 0 {
			parts = append(parts, "--data", quoteShell(string(req.Body)))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func collectForwardHeaders(session any) map[string]string {
	root, ok := session.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	headers := map[string]string{}
	if cookie := strings.TrimSpace(fmt.Sprint(root["cookie"])); cookie != "" && cookie != "<nil>" {
		headers["Cookie"] = cookie
	}
	if rawHeaders, ok := root["headers"].(map[string]any); ok {
		keys := make([]string, 0, len(rawHeaders))
		for key := range rawHeaders {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if shouldSkipForwardHeader(key) {
				continue
			}
			value := strings.TrimSpace(fmt.Sprint(rawHeaders[key]))
			if value != "" && value != "<nil>" {
				headers[canonicalHeaderKey(key)] = value
			}
		}
	}
	return headers
}

func responseStatus(resp *httpclient.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func canonicalHeaderKey(key string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "-")
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func quoteShell(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func redactHeaderValue(key string, value string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "cookie" || strings.Contains(lower, "token") || strings.Contains(lower, "auth") || strings.Contains(lower, "sign") {
		if lower == "cookie" {
			return "<redacted-cookie>"
		}
		if len(value) <= 8 {
			return "<redacted>"
		}
		return value[:4] + "..." + value[len(value)-4:]
	}
	return value
}

func authResponseSucceeded(payload map[string]any) bool {
	if len(payload) == 0 {
		return false
	}
	// valid 是校验接口最直接的成功信号。
	// 只要响应里显式给了 valid，就优先使用，避免“code 缺失但 valid=false”被误判成成功。
	if valid, ok := recursiveBoolValue(payload, "valid", "Valid"); ok {
		return valid
	}
	// 校验接口的成功语义并不稳定：有的返回 code/status，有的只给 valid。
	// 这里先吃通用成功码，再回退到 valid，尽量贴近“校验通过即可继续 merge session”的原始意图。
	code := strings.TrimSpace(recursiveStringValue(
		payload,
		"code", "Code",
		"ret", "Ret",
		"errno", "err_no", "errNo", "ErrNo",
		"status_code", "statusCode", "StatusCode",
		"status", "Status",
	))
	switch strings.ToLower(code) {
	case "0", "200", "ok", "success", "true":
		return true
	}
	return false
}

func mergeValidatedSession(session any, payload map[string]any) any {
	root, ok := session.(map[string]any)
	if !ok {
		return session
	}
	out := map[string]any{}
	for key, value := range root {
		out[key] = value
	}
	// 校验接口的真实响应会把身份信息包在 session / identity / user 等不同 wrapper 里，
	// 这里统一递归提取，避免 wrapper 变化导致 user_id/workspace_id 丢失。
	for _, spec := range []sessionWrapperSpec{
		{Keys: []string{"session", "Session"}},
		{Keys: []string{"identity", "Identity"}},
		{Keys: []string{"user", "User"}, CanonicalIDKey: "user_id", CanonicalNameKey: "display_name"},
		{Keys: []string{"member", "Member"}, CanonicalIDKey: "user_id", CanonicalNameKey: "display_name"},
		{Keys: []string{"profile", "Profile"}, CanonicalIDKey: "user_id", CanonicalNameKey: "display_name"},
		{Keys: []string{"account", "Account"}},
		{Keys: []string{"workspace", "Workspace"}, CanonicalIDKey: "workspace_id"},
		{Keys: []string{"space", "Space"}, CanonicalIDKey: "space_id"},
		{Keys: []string{"team", "Team"}, CanonicalIDKey: "team_id"},
		{Keys: []string{"tenant", "Tenant"}, CanonicalIDKey: "tenant_id"},
	} {
		if sessionMap, ok := firstNestedMapValue(payload, spec.Keys...); ok {
			mergeNonEmptySessionFields(out, sessionMap)
			backfillSessionWrapperAliases(out, sessionMap, spec)
		}
	}
	for _, key := range []string{
		"uid", "UID",
		"user_id", "userId", "UserId", "UserID",
		"display_name", "displayName", "DisplayName",
		"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID",
		"space_id", "spaceId", "SpaceId", "SpaceID",
		"team_id", "teamId", "TeamId", "TeamID",
		"tenant_id", "tenantId", "TenantId", "TenantID",
	} {
		if value := recursiveStringValue(payload, key); value != "" {
			if existing, ok := out[key]; !ok || strings.TrimSpace(fmt.Sprint(existing)) == "" {
				out[key] = value
			}
		}
	}
	// 上面这轮递归会把远端原始字段尽量保留下来；
	// 这里再做一轮 canonical 回填，保证后续只读统一键名的调用侧也能稳定工作。
	backfillValidatedSessionCanonicalAliases(out)
	return out
}

type sessionWrapperSpec struct {
	Keys             []string
	CanonicalIDKey   string
	CanonicalNameKey string
}

func backfillValidatedSessionCanonicalAliases(session map[string]any) {
	if len(session) == 0 {
		return
	}
	// Validate 接口返回经常混用 UID/UserID/WorkspaceID 等原始别名。
	// 这里在 merge 结束后统一补齐 canonical 字段，避免登录后续链路继续感知大小写/命名差异。
	for _, spec := range []struct {
		Canonical string
		Keys      []string
	}{
		{Canonical: "user_id", Keys: []string{"user_id", "userId", "UserId", "UserID", "uid", "UID"}},
		{Canonical: "display_name", Keys: []string{"display_name", "displayName", "DisplayName", "name", "Name"}},
		{Canonical: "workspace_id", Keys: []string{"workspace_id", "workspaceId", "WorkspaceId", "WorkspaceID"}},
		{Canonical: "space_id", Keys: []string{"space_id", "spaceId", "SpaceId", "SpaceID"}},
		{Canonical: "team_id", Keys: []string{"team_id", "teamId", "TeamId", "TeamID"}},
		{Canonical: "tenant_id", Keys: []string{"tenant_id", "tenantId", "TenantId", "TenantID"}},
	} {
		if existing := strings.TrimSpace(fmt.Sprint(session[spec.Canonical])); existing != "" && existing != "<nil>" {
			continue
		}
		if value := firstStringValue(session, spec.Keys...); value != "" {
			session[spec.Canonical] = value
		}
	}
}

func backfillSessionWrapperAliases(dst map[string]any, src map[string]any, spec sessionWrapperSpec) {
	if len(dst) == 0 || len(src) == 0 {
		return
	}
	if strings.TrimSpace(spec.CanonicalIDKey) != "" {
		// wrapper 内部的 ID 回填只认“本 wrapper 的 canonical 字段”和通用 id。
		// 这样 user/workspace/space/team/tenant 之间不会再因为字段名相似或历史混用而互相串值。
		idKeys := []string{spec.CanonicalIDKey, "id", "ID"}
		switch spec.CanonicalIDKey {
		case "user_id":
			idKeys = append([]string{spec.CanonicalIDKey, "uid", "UID", "userId", "UserId", "UserID"}, idKeys[1:]...)
		case "workspace_id":
			idKeys = append([]string{spec.CanonicalIDKey, "workspaceId", "WorkspaceId", "WorkspaceID"}, idKeys[1:]...)
		case "space_id":
			idKeys = append([]string{spec.CanonicalIDKey, "spaceId", "SpaceId", "SpaceID"}, idKeys[1:]...)
		case "team_id":
			idKeys = append([]string{spec.CanonicalIDKey, "teamId", "TeamId", "TeamID"}, idKeys[1:]...)
		case "tenant_id":
			idKeys = append([]string{spec.CanonicalIDKey, "tenantId", "TenantId", "TenantID"}, idKeys[1:]...)
		}
		if value := firstStringValue(src, idKeys...); value != "" {
			if existing := strings.TrimSpace(fmt.Sprint(dst[spec.CanonicalIDKey])); existing == "" || existing == "<nil>" {
				dst[spec.CanonicalIDKey] = value
			}
		}
	}
	if strings.TrimSpace(spec.CanonicalNameKey) != "" {
		// display_name/name 只在 wrapper 内部回填，不向外层跨 wrapper 乱取，
		// 避免用户昵称、团队名称、空间名称混成同一个显示字段。
		if value := firstStringValue(src,
			spec.CanonicalNameKey,
			"display_name", "displayName", "DisplayName",
			"name", "Name",
		); value != "" {
			if existing := strings.TrimSpace(fmt.Sprint(dst[spec.CanonicalNameKey])); existing == "" || existing == "<nil>" {
				dst[spec.CanonicalNameKey] = value
			}
		}
	}
}

func mergeNonEmptySessionFields(dst map[string]any, src map[string]any) {
	// merge 只吸收显式非空字段，避免深层 wrapper 里的空值把外层已确认可用的 session 字段覆盖掉。
	for key, value := range src {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			dst[key] = value
		}
	}
}

func firstNestedMapValue(root any, keys ...string) (map[string]any, bool) {
	var visit func(any, int) (map[string]any, bool)
	visit = func(node any, depth int) (map[string]any, bool) {
		if depth > 6 {
			return nil, false
		}
		current, ok := node.(map[string]any)
		if !ok || len(current) == 0 {
			return nil, false
		}
		for _, key := range keys {
			if value, ok := current[key].(map[string]any); ok && len(value) > 0 {
				return value, true
			}
		}
		// Validate 响应常见 Data/Result/Response/Payload 多层 wrapper。
		// 这里优先沿这些高频壳继续向下找，减少“命中了字段但被外层包装挡住”的情况。
		for _, childKey := range []string{"data", "Data", "result", "Result", "response", "Response", "payload", "Payload"} {
			if value, ok := visit(current[childKey], depth+1); ok {
				return value, true
			}
		}
		for _, child := range current {
			if value, ok := visit(child, depth+1); ok {
				return value, true
			}
		}
		return nil, false
	}
	return visit(root, 0)
}

func recursiveStringValue(root any, keys ...string) string {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) string
	visit = func(node any, depth int) string {
		if depth > 6 {
			return ""
		}
		switch current := node.(type) {
		case map[string]any:
			for key, value := range current {
				if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; ok {
					text := strings.TrimSpace(fmt.Sprint(value))
					if text != "" && text != "<nil>" {
						return text
					}
				}
			}
			// 只有当前层没命中时才继续递归，保持“近层字段优先于深层 fallback”的读取顺序，
			// 避免 wrapper 里旧值把更外层已经标准化的新值盖回去。
			for _, child := range current {
				if text := visit(child, depth+1); text != "" {
					return text
				}
			}
		case []any:
			for _, child := range current {
				if text := visit(child, depth+1); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return visit(root, 0)
}

func recursiveBoolValue(root any, keys ...string) (bool, bool) {
	lookup := map[string]struct{}{}
	for _, key := range keys {
		lookup[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	var visit func(any, int) (bool, bool)
	visit = func(node any, depth int) (bool, bool) {
		if depth > 6 {
			return false, false
		}
		switch current := node.(type) {
		case map[string]any:
			for key, value := range current {
				if _, ok := lookup[strings.ToLower(strings.TrimSpace(key))]; !ok {
					continue
				}
				switch typed := value.(type) {
				case bool:
					return typed, true
				case string:
					text := strings.TrimSpace(typed)
					if text == "" {
						continue
					}
					// 部分响应把 true/false 写成字符串；这里只把非空字符串解释成显式布尔值，
					// 避免校验接口“给了 valid 但类型漂移”时整条登录校验链路误判失败。
					return strings.EqualFold(text, "true"), true
				}
			}
			for _, child := range current {
				if value, ok := visit(child, depth+1); ok {
					return value, true
				}
			}
		case []any:
			for _, child := range current {
				if value, ok := visit(child, depth+1); ok {
					return value, true
				}
			}
		}
		return false, false
	}
	return visit(root, 0)
}

func firstMapValue(root map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := root[key].(map[string]any); ok {
			return value, true
		}
	}
	return nil, false
}

func firstStringValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := root[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	if data, ok := firstMapValue(root, "data", "Data", "result", "Result", "response", "Response", "payload", "Payload"); ok {
		for _, key := range keys {
			if value, ok := data[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
	}
	return ""
}
