package cmd

import (
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

func TestParseLoginRunOptionsDefaultsToFixedPort(t *testing.T) {
	t.Helper()

	opts, err := parseLoginRunOptions("login", nil)
	if err != nil {
		t.Fatalf("parseLoginRunOptions failed: %v", err)
	}
	if opts.Port != config.DefaultLoginCallbackPort {
		t.Fatalf("expected fixed callback port %d, got %d", config.DefaultLoginCallbackPort, opts.Port)
	}
	if opts.Headless || opts.Debug {
		t.Fatalf("unexpected default login options: %#v", opts)
	}
}

func TestParseLoginRunOptionsParsesKnownFlags(t *testing.T) {
	t.Helper()

	opts, err := parseLoginRunOptions("login", []string{"--headless", "--debug"})
	if err != nil {
		t.Fatalf("parseLoginRunOptions failed: %v", err)
	}
	if !opts.Headless || !opts.Debug {
		t.Fatalf("expected known flags to be enabled: %#v", opts)
	}
}

func TestParseLoginRunOptionsRejectsPortFlag(t *testing.T) {
	t.Helper()

	_, err := parseLoginRunOptions("login", []string{"--port=45678"})
	if err == nil {
		t.Fatalf("expected parseLoginRunOptions failure")
	}
	if err.Error() != "unknown flag: --port" {
		t.Fatalf("unexpected error: %v", err)
	}
}
