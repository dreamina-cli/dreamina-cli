package login

import (
	"bytes"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

func TestParseRunInputsDefaultsToFixedPort(t *testing.T) {
	t.Helper()

	relogin, opts, out := parseRunInputs()
	if relogin {
		t.Fatalf("unexpected relogin default")
	}
	if opts.Port != config.DefaultLoginCallbackPort {
		t.Fatalf("expected fixed callback port %d, got %d", config.DefaultLoginCallbackPort, opts.Port)
	}
	if out == nil {
		t.Fatalf("expected default writer")
	}
}

func TestParseRunInputsNormalizesExplicitZeroPortToFixedDefault(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	_, opts, gotWriter := parseRunInputs(RunOptions{Port: 0, Headless: true}, &out)
	if opts.Port != config.DefaultLoginCallbackPort {
		t.Fatalf("expected explicit zero port to normalize to %d, got %d", config.DefaultLoginCallbackPort, opts.Port)
	}
	if !opts.Headless {
		t.Fatalf("expected headless option to be preserved: %#v", opts)
	}
	if gotWriter != &out {
		t.Fatalf("unexpected writer: %T", gotWriter)
	}
}
