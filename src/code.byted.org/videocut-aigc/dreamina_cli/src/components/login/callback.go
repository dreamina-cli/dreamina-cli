package login

import (
	"fmt"
	"net/http"
	"strings"
)

type callbackPayload struct {
	Qrcode string `json:"qrcode"`
	Token  string `json:"token"`
}

type httpResponseWriter interface {
	Headers() any
	WriteStatus(code int)
	WriteBody(b []byte)
	WriteError(err error)
}

type stdHTTPResponseWriter struct {
	w           http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func newStdHTTPResponseWriter(v ...any) *stdHTTPResponseWriter {
	writer := &stdHTTPResponseWriter{}
	for _, arg := range v {
		if rw, ok := arg.(http.ResponseWriter); ok && rw != nil {
			writer.w = rw
			break
		}
	}
	return writer
}

func (w *stdHTTPResponseWriter) Headers() any {
	if w == nil || w.w == nil {
		return http.Header{}
	}
	return w.w.Header()
}

func (w *stdHTTPResponseWriter) WriteStatus(code int) {
	if w == nil || w.w == nil || w.wroteHeader {
		return
	}
	if code <= 0 {
		code = http.StatusOK
	}
	w.statusCode = code
	w.wroteHeader = true
	w.w.WriteHeader(code)
}

func (w *stdHTTPResponseWriter) WriteBody(b []byte) {
	if w == nil || w.w == nil {
		return
	}
	if !w.wroteHeader {
		w.WriteStatus(http.StatusOK)
	}
	if len(b) == 0 {
		return
	}
	_, _ = w.w.Write(b)
}

func (w *stdHTTPResponseWriter) WriteError(err error) {
	if w == nil || w.w == nil {
		return
	}
	status := w.statusCode
	if status <= 0 || status == http.StatusOK {
		status = http.StatusInternalServerError
	}
	if header := w.w.Header(); header.Get("Content-Type") == "" {
		header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.statusCode = status
	if !w.wroteHeader {
		w.wroteHeader = true
		w.w.WriteHeader(status)
	}
	if err != nil {
		_, _ = w.w.Write([]byte(err.Error()))
	}
}

func (m *Manager) CallbackHandler() http.Handler {
	// 本地回调服务统一挂载到保存登录会话的固定路由上。
	return m.withCallbackCORS(http.HandlerFunc(m.handleSaveSession))
}

func (m *Manager) withCallbackCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.applyCallbackCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) handleSaveSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := stringsToLower(r.Header.Get("Content-Type"))
	if !stringsHasPrefix(contentType, "application/json") {
		http.Error(w, "content type must be application/json", http.StatusBadRequest)
		return
	}

	body, err := readRequestBodyLimit(r, 0xA00000)
	if err != nil {
		http.Error(w, "read request body failed", http.StatusBadRequest)
		return
	}

	if err := m.ImportLoginResponseJSON(body); err != nil {
		status := http.StatusInternalServerError
		if looksLikeUserInputError(err) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "saved",
	})
}

func (m *Manager) applyCallbackCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	loginOrigin := m.loginOrigin()

	// 只有来源与登录站点一致时才放行 CORS，其他来源只记录日志不返回放行头。
	if origin == "" || origin != loginOrigin {
		logCallbackOriginMismatch(origin, loginOrigin, r)
		return
	}

	allowHeaders := r.Header.Get("Access-Control-Request-Headers")
	if allowHeaders == "" {
		allowHeaders = "Content-Type"
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", allowHeaders)
	h.Set("Access-Control-Allow-Private-Network", "true")
	h.Add("Vary", "Origin")
	h.Add("Vary", "Access-Control-Request-Headers")
	h.Add("Vary", "Access-Control-Request-Private-Network")
}

func (m *Manager) setLoginFailure(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loginFailure = err
	m.loginCompleted = false
	return err
}

func buildLoginFailure(cause any) error {
	switch value := cause.(type) {
	case nil:
		return nil
	case error:
		return value
	default:
		return importFailureFromAny(value)
	}
}

func appendLogID(msg string, logID string) string {
	if logID == "" {
		return msg
	}
	return msg + " log_id=" + logID
}

func loginFailureLogID(v any) string {
	return strings.TrimSpace(findFirstStringByKeys(
		v,
		"log_id",
		"logid",
		"logId",
		"LogID",
		"request_id",
		"requestId",
		"RequestId",
		"RequestID",
	))
}

func formatUserVisibleLoginFailure(logID string) error {
	msg := "login error , please 联系客服"
	if logID != "" {
		msg += "，logid = " + logID
	}
	return fmt.Errorf("%s", msg)
}

func validateLoginResponse(v ...any) error {
	// 导入前先校验响应体是否至少包含原始 callback 成功体里的核心凭证字段。
	// 实测成功回调体会返回 auth_token / auto_token_md5_sign / sign_key_pair_name，
	// random_secret_key 则来自本地预写入状态，而不是一定由浏览器回调再次回传。
	payload := firstAny(v...)
	fields := importFieldsFromAny(payload)
	if fields.AuthToken == "" {
		if failure := importFailureFromAny(payload); failure != nil {
			return failure
		}
		return fmt.Errorf("auth_token is required")
	}
	if fields.AutoTokenMD5Sign == "" {
		return fmt.Errorf("auto_token_md5_sign is required")
	}
	if fields.SignKeyPairName == "" {
		return fmt.Errorf("sign_key_pair_name is required")
	}
	return nil
}

func sanitizeSessionHeaders(header http.Header) http.Header {
	sanitized := make(http.Header)
	for _, key := range []string{
		"Cookie",
		"Host",
		"Origin",
		"Referer",
		"User-Agent",
		"Accept",
		"Accept-Language",
		"Accept-Encoding",
		"Appid",
		"Pf",
		"Appvr",
		"Device-Time",
		"Lan",
		"Sign",
		"Sign-Ver",
		"X-Forwarded-For",
		"X-Real-IP",
		"X-Request-Id",
		"X-Tt-Logid",
		"X-Tt-Trace-Id",
		"Sec-Ch-Ua",
		"Sec-Ch-Ua-Mobile",
		"Sec-Ch-Ua-Platform",
		"Sec-Fetch-Site",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Dest",
		"Priority",
	} {
		copySanitizedHeader(sanitized, header, key)
	}
	return sanitized
}

func copySanitizedHeader(dst http.Header, src http.Header, key string) {
	if values, ok := src[key]; ok && len(values) > 0 {
		dst[key] = append([]string(nil), values...)
	}
}

func (m *Manager) callbackAllowedOrigin() string { return m.loginOrigin() }

func importFailureFromAny(v any) error {
	if v == nil {
		return nil
	}
	fields := importFieldsFromAny(v)
	if fields.AuthToken != "" {
		return nil
	}
	logID := loginFailureLogID(v)

	message := strings.TrimSpace(findFirstStringByKeys(
		v,
		"message",
		"msg",
		"error",
		"err_msg",
		"errmsg",
		"description",
	))
	if message == "" && logID == "" {
		return nil
	}
	return formatUserVisibleLoginFailure(logID)
}

func firstAny(v ...any) any {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}
