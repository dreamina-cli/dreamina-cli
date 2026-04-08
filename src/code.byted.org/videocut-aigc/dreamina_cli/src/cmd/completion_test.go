package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionHelpAligned(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"completion", "-h"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"Usage:\n  dreamina completion [flags]",
		"Generate the autocompletion script for dreamina for the specified shell.",
		"See each sub-command's help for details on how to use the generated script.",
		"  -h, --help   help for completion",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("completion help missing %q: %q", want, text)
		}
	}
}

func TestCompletionUnknownShellFallsBackToHelp(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"completion", "x"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	if text := out.String(); !strings.Contains(text, "Usage:\n  dreamina completion [flags]") {
		t.Fatalf("unexpected completion fallback output: %q", text)
	}
}

func TestCompletionShellScriptsAvailable(t *testing.T) {
	t.Helper()

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"completion", "bash"}, want: "# bash completion for dreamina"},
		{args: []string{"completion", "zsh"}, want: "#compdef dreamina"},
		{args: []string{"completion", "fish"}, want: "function __dreamina_complete"},
		{args: []string{"completion", "powershell"}, want: "Register-ArgumentCompleter -CommandName 'dreamina'"},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		var out bytes.Buffer
		root.out = &out
		root.SetArgs(tc.args)

		if _, err := root.ExecuteC(); err != nil {
			t.Fatalf("ExecuteC(%v) failed: %v", tc.args, err)
		}
		if text := out.String(); !strings.Contains(text, tc.want) {
			t.Fatalf("unexpected completion script for %v: %q", tc.args, text)
		}
	}
}

func TestCompleteCommandAlignedForRootAndFlags(t *testing.T) {
	t.Helper()

	cases := []struct {
		args     []string
		contains []string
	}{
		{
			args: []string{"__complete", ""},
			contains: []string{
				"completion\tGenerate the autocompletion script for the specified shell",
				"text2video\tSubmit a Dreamina text-to-video task",
				":4",
			},
		},
		{
			args: []string{"__complete", "completion", "p"},
			contains: []string{
				"powershell\tGenerate the autocompletion script for powershell",
				":4",
			},
		},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		var out bytes.Buffer
		root.out = &out
		root.SetArgs(tc.args)

		if _, err := root.ExecuteC(); err != nil {
			t.Fatalf("ExecuteC(%v) failed: %v", tc.args, err)
		}
		text := out.String()
		for _, want := range tc.contains {
			if !strings.Contains(text, want) {
				t.Fatalf("completion output for %v missing %q: %q", tc.args, want, text)
			}
		}
	}
}

func TestRef2VideoHelpAlignedAndDeprecatedBannerShownOnDirectHelp(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"ref2video", "-h"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		`Command "ref2video" is deprecated, use multiframe2video instead`,
		"Usage:\n  dreamina ref2video [flags]",
		"Upload multiple local images, then submit a Dreamina intelligent multi-frame video task for coherent visual storytelling.",
		`dreamina ref2video --images ./a.png,./b.png --prompt="character turns around"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ref2video help missing %q: %q", want, text)
		}
	}
}

func TestHelpRef2VideoAlignedWithoutDeprecatedBanner(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"help", "ref2video"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if strings.Contains(text, `Command "ref2video" is deprecated, use multiframe2video instead`) {
		t.Fatalf("did not expect deprecated banner in help ref2video: %q", text)
	}
	if !strings.Contains(text, "Usage:\n  dreamina ref2video [flags]") {
		t.Fatalf("unexpected help ref2video output: %q", text)
	}
}
