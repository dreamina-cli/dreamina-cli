package login

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/logging"
)

func fmtErrorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func bytesNewReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}

func isNotExist(err error) bool {
	return os.IsNotExist(err)
}

func stringsToLower(s string) string {
	return strings.ToLower(s)
}

func stringsHasPrefix(s string, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

func readRequestBodyLimit(r *http.Request, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, limit))
}

func looksLikeUserInputError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "required") ||
		strings.Contains(msg, "empty")
}

func logCallbackOriginMismatch(origin string, loginOrigin string, r *http.Request) {
	if r == nil {
		return
	}
	headers := sanitizeSessionHeaders(r.Header)
	headerView := map[string]string{}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := headers.Values(key)
		if len(values) == 0 {
			continue
		}
		headerView[key] = strings.Join(values, ", ")
	}
	body, _ := json.Marshal(headerView)
	logging.InfofContext(
		r.Context(),
		"[applyCallbackCORSHeaders] skip cors method=%s path=%s host=%q origin=%q allowed_origin=%q access_control_request_headers=%q remote_addr=%q user_agent=%q headers=%s",
		r.Method,
		r.URL.Path,
		r.Host,
		strings.TrimSpace(origin),
		strings.TrimSpace(loginOrigin),
		strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")),
		r.RemoteAddr,
		strings.TrimSpace(r.UserAgent()),
		string(body),
	)
}

func writeJSON(w any, status int, body any) error {
	rw, ok := w.(http.ResponseWriter)
	if !ok {
		return nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_, err = rw.Write(data)
	return err
}
