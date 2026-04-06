package envsetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyCreatesConfiguredDirsAndSetsEnvWhenMissing(t *testing.T) {
	t.Helper()

	customDir := filepath.Join(t.TempDir(), "dreamina-config")
	t.Setenv("DREAMINA_CONFIG_DIR", customDir)

	if err := Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := os.Getenv("DREAMINA_CONFIG_DIR"); got != customDir {
		t.Fatalf("DREAMINA_CONFIG_DIR = %q, want %q", got, customDir)
	}
	for _, path := range []string{customDir, filepath.Join(customDir, "logs")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected directory: %s", path)
		}
	}
}

func TestApplyPreservesExistingConfigDirEnv(t *testing.T) {
	t.Helper()

	preexisting := filepath.Join(t.TempDir(), "already-set")
	t.Setenv("DREAMINA_CONFIG_DIR", preexisting)

	// Apply should honor the existing env instead of overwriting it.

	if err := Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := os.Getenv("DREAMINA_CONFIG_DIR"); got != preexisting {
		t.Fatalf("DREAMINA_CONFIG_DIR = %q, want %q", got, preexisting)
	}
}
