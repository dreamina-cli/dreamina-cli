package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireLoginAcceptsCookieSessionWithoutCredential(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg dir: %v", err)
	}
	body := []byte("{\n  \"cookie\": \"sid=test-cookie-only\",\n  \"uid\": \"71890275940283\"\n}\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "cookie.json"), body, 0o600); err != nil {
		t.Fatalf("write cookie.json: %v", err)
	}
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	ctx, err := NewContext()
	if err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := ctx.RequireLogin(); err != nil {
		t.Fatalf("RequireLogin should accept cookie-only session: %v", err)
	}
}
