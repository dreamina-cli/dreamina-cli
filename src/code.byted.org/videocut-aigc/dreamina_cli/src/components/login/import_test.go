package login

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportLoginResponseJSONRequiresSignatureFields(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)
	authToken := encryptAuthTokenForTest(t, []byte(`{"cookie":"sid=test","headers":{"User-Agent":"ua-test"}}`), "secret-key")
	body, err := json.Marshal(map[string]any{
		"auth_token": authToken,
	})
	if err != nil {
		t.Fatalf("marshal login response: %v", err)
	}

	err = mgr.ImportLoginResponseJSON(body)
	if err == nil {
		t.Fatalf("expected ImportLoginResponseJSON failure")
	}
	if !strings.Contains(err.Error(), "auto_token_md5_sign is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportLoginResponseJSONRejectsInvalidJSONWithOriginalMessage(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)

	err := mgr.ImportLoginResponseJSON([]byte("{not-json}\n"))
	if err == nil {
		t.Fatalf("expected ImportLoginResponseJSON failure")
	}
	if got := err.Error(); got != "request body must be valid json" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestImportLoginResponseJSONUsesLocallyPreparedSecretKey(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	mgr := &Manager{
		dir:            dir,
		credentialPath: filepath.Join(dir, "credential.json"),
		loginBaseURL:   "https://jimeng.jianying.com/ai-tool/login",
	}
	if err := mgr.saveCredential(&Credential{
		RandomSecretKey: "secret-key",
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	authToken := encryptAuthTokenForTest(t, []byte(`{"cookie":"sid=test","headers":{"User-Agent":"ua-test"}}`), "secret-key")
	body, err := json.Marshal(map[string]any{
		"ret":                 "0",
		"msg":                 "success",
		"auth_token":          authToken,
		"auto_token_md5_sign": "dummy-signature",
		"sign_key_pair_name":  "v0.0.1-idx2",
	})
	if err != nil {
		t.Fatalf("marshal login response: %v", err)
	}

	if err := mgr.ImportLoginResponseJSON(body); err != nil {
		t.Fatalf("ImportLoginResponseJSON failed: %v", err)
	}

	cred, err := mgr.loadCredential()
	if err != nil {
		t.Fatalf("loadCredential failed: %v", err)
	}
	if cred.RandomSecretKey != "secret-key" {
		t.Fatalf("unexpected saved random_secret_key: %#v", cred.RandomSecretKey)
	}
	if cred.AuthToken != authToken || cred.AutoTokenMD5Sign != "dummy-signature" || cred.SignKeyPairName != "v0.0.1-idx2" {
		t.Fatalf("unexpected saved credential: %#v", cred)
	}
}

func TestImportLoginResponseJSONFormatsLoginFailureLikeOriginalCLI(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)

	body, err := json.Marshal(map[string]any{
		"message": "not vip error",
		"logid":   "2026040521281475660C1F3D26F0818742",
	})
	if err != nil {
		t.Fatalf("marshal login failure: %v", err)
	}

	err = mgr.ImportLoginResponseJSON(body)
	if err == nil {
		t.Fatalf("expected ImportLoginResponseJSON failure")
	}
	want := "login error , please 联系客服，logid = 2026040521281475660C1F3D26F0818742"
	if err.Error() != want {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestImportLoginResponseJSONFormatsLoginFailureWithoutLogID(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)

	body, err := json.Marshal(map[string]any{
		"message": "not vip error",
	})
	if err != nil {
		t.Fatalf("marshal login failure: %v", err)
	}

	err = mgr.ImportLoginResponseJSON(body)
	if err == nil {
		t.Fatalf("expected ImportLoginResponseJSON failure")
	}
	want := "login error , please 联系客服"
	if err.Error() != want {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}
