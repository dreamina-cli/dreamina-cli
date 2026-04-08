package login

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCallbackHandlerOptionsSetsCORSForAllowedOrigin(t *testing.T) {
	t.Helper()

	mgr := newTestLoginManager(t)
	req := httptest.NewRequest(http.MethodOptions, "/dreamina/callback/save_session", nil)
	req.Header.Set("Origin", "https://jimeng.jianying.com")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Test")
	rec := httptest.NewRecorder()

	mgr.CallbackHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://jimeng.jianying.com" {
		t.Fatalf("unexpected allow origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Test" {
		t.Fatalf("unexpected allow headers: %q", got)
	}
}

func TestCallbackHandlerOptionsSkipsCORSForUnexpectedOrigin(t *testing.T) {
	t.Helper()

	mgr := newTestLoginManager(t)
	req := httptest.NewRequest(http.MethodOptions, "/dreamina/callback/save_session", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	mgr.CallbackHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow origin: %q", got)
	}
}

func TestCallbackHandlerPostSavesCredential(t *testing.T) {
	t.Helper()

	mgr := newTestLoginManager(t)
	authToken := "token"
	body, err := json.Marshal(map[string]any{
		"auth_token":          authToken,
		"auto_token_md5_sign": "dummy-signature",
		"sign_key_pair_name":  "v0.0.1-idx2",
		"random_secret_key":   "secret-key",
	})
	if err != nil {
		t.Fatalf("marshal login response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/dreamina/callback/save_session", io.NopCloser(bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://jimeng.jianying.com")
	rec := httptest.NewRecorder()

	mgr.CallbackHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%q", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "saved" {
		t.Fatalf("unexpected response payload: %#v", resp)
	}
	completed, err := mgr.LoginCompleted()
	if err != nil {
		t.Fatalf("LoginCompleted failed: %v", err)
	}
	if !completed {
		t.Fatalf("expected loginCompleted=true")
	}
	cred, err := mgr.loadCredential()
	if err != nil {
		t.Fatalf("loadCredential failed: %v", err)
	}
	if cred.AuthToken != authToken {
		t.Fatalf("unexpected saved auth token")
	}
	if cred.RandomSecretKey != "secret-key" {
		t.Fatalf("unexpected saved random_secret_key: %q", cred.RandomSecretKey)
	}
	if cred.AutoTokenMD5Sign != "dummy-signature" || cred.SignKeyPairName != "v0.0.1-idx2" {
		t.Fatalf("unexpected saved signature fields: %#v", cred)
	}
}

func TestCallbackHandlerRejectsInvalidContentType(t *testing.T) {
	t.Helper()

	mgr := newTestLoginManager(t)
	req := httptest.NewRequest(http.MethodPost, "/dreamina/callback/save_session", io.NopCloser(bytes.NewReader([]byte(`{}`))))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	mgr.CallbackHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestSanitizeSessionHeadersKeepsSecChUaHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("Sec-Ch-Ua", `"Chromium";v="146"`)
	headers.Set("Sec-Ch-Ua-Mobile", "?0")
	headers.Set("Sec-Ch-Ua-Platform", `"Windows"`)

	got := sanitizeSessionHeaders(headers)

	if got.Get("Sec-Ch-Ua") != `"Chromium";v="146"` {
		t.Fatalf("unexpected Sec-Ch-Ua: %q", got.Get("Sec-Ch-Ua"))
	}
	if got.Get("Sec-Ch-Ua-Mobile") != "?0" {
		t.Fatalf("unexpected Sec-Ch-Ua-Mobile: %q", got.Get("Sec-Ch-Ua-Mobile"))
	}
	if got.Get("Sec-Ch-Ua-Platform") != `"Windows"` {
		t.Fatalf("unexpected Sec-Ch-Ua-Platform: %q", got.Get("Sec-Ch-Ua-Platform"))
	}
}

func newTestLoginManager(t *testing.T) *Manager {
	t.Helper()

	dir := t.TempDir()
	return &Manager{
		dir:            dir,
		credentialPath: filepath.Join(dir, "credential.json"),
		loginBaseURL:   "https://jimeng.jianying.com/ai-tool/login",
	}
}
