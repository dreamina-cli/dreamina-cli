package config

import (
	"path/filepath"
	"testing"
)

func TestDirPrefersDreaminaConfigDirEnv(t *testing.T) {
	t.Helper()

	customDir := filepath.Join(t.TempDir(), "dreamina-config")
	t.Setenv("DREAMINA_CONFIG_DIR", customDir)

	if got := Dir(); got != customDir {
		t.Fatalf("Dir() = %q, want %q", got, customDir)
	}
	if got := TaskDBPath(); got != filepath.Join(customDir, "tasks.db") {
		t.Fatalf("TaskDBPath() = %q", got)
	}
	if got := LogsDir(); got != filepath.Join(customDir, "logs") {
		t.Fatalf("LogsDir() = %q", got)
	}
}

func TestLoadUsesEnvBackedConfigDir(t *testing.T) {
	t.Helper()

	customDir := filepath.Join(t.TempDir(), "dreamina-config")
	t.Setenv("DREAMINA_CONFIG_DIR", customDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Dir != customDir {
		t.Fatalf("cfg.Dir = %q, want %q", cfg.Dir, customDir)
	}
	if cfg.CredentialPath != filepath.Join(customDir, "credential.json") {
		t.Fatalf("unexpected credential path: %q", cfg.CredentialPath)
	}
	if cfg.TaskDBPathValue != filepath.Join(customDir, "tasks.db") {
		t.Fatalf("unexpected task db path: %q", cfg.TaskDBPathValue)
	}
}
