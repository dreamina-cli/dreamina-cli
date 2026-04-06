package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpShownForNoArgs(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs(nil)

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Built-in Commands:") {
		t.Fatalf("expected built-in commands section: %q", text)
	}
	if !strings.Contains(text, "Generator Commands:") {
		t.Fatalf("expected generator commands section: %q", text)
	}
	if !strings.Contains(text, "help                 Help about any command") {
		t.Fatalf("expected help row in root help: %q", text)
	}
	if strings.Contains(text, "validate-auth-token") {
		t.Fatalf("did not expect hidden validate-auth-token in root help: %q", text)
	}
	if strings.Contains(text, "ref2video") {
		t.Fatalf("did not expect alias ref2video in root help: %q", text)
	}
}

func TestRootHelpShownForHelpFlag(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"--help"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Usage:\n  dreamina [flags]") {
		t.Fatalf("unexpected root help output: %q", text)
	}
}

func TestLoginHelpShownForHelpFlag(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"login", "-h"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Usage:\n  dreamina login [flags]") {
		t.Fatalf("unexpected login help output: %q", text)
	}
	if !strings.Contains(text, "Flags:") || !strings.Contains(text, "--headless") {
		t.Fatalf("expected login flags in help output: %q", text)
	}
	if !strings.Contains(text, "Examples:") || !strings.Contains(text, "dreamina login --headless") {
		t.Fatalf("expected login examples in help output: %q", text)
	}
}

func TestHelpCommandSelfHelpAligned(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"help", "help"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Usage:\n  dreamina help [command] [flags]") {
		t.Fatalf("unexpected help help output: %q", text)
	}
	if !strings.Contains(text, "Help provides help for any command in the application.") {
		t.Fatalf("missing help summary: %q", text)
	}
	if !strings.Contains(text, "  -h, --help   help for help") {
		t.Fatalf("missing help flag line: %q", text)
	}
}

func TestUnknownHelpTopicFallsBackToOriginalStyleRootHelp(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"help", "query_result_download"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Unknown help topic [`query_result_download`]") {
		t.Fatalf("missing unknown help topic banner: %q", text)
	}
	if !strings.Contains(text, "  dreamina [command]") {
		t.Fatalf("missing root fallback usage: %q", text)
	}
	if !strings.Contains(text, "Additional Commands:\n  completion            Generate the autocompletion script for the specified shell") {
		t.Fatalf("missing additional commands section: %q", text)
	}
	if strings.Contains(text, "validate-auth-token") || strings.Contains(text, "ref2video") {
		t.Fatalf("unexpected hidden command leaked into unknown help fallback: %q", text)
	}
}

func TestGeneratorHelpAlignedForHelpSubcommand(t *testing.T) {
	t.Helper()

	cases := []struct {
		args     []string
		contains []string
	}{
		{
			args: []string{"help", "text2video"},
			contains: []string{
				"Submit a Dreamina text-to-video task.",
				"model_version: seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip",
				`dreamina text2video --prompt="a cat running" --duration=5`,
			},
		},
		{
			args: []string{"help", "image2video"},
			contains: []string{
				"For multi-image storytelling, use multiframe2video; for full-reference mixed-media generation, use multimodal2video.",
				"advanced model_version values: 3.0, 3.0fast, 3.0pro, 3.0_fast, 3.0_pro, 3.5pro, 3.5_pro, seedance2.0, seedance2.0fast, seedance2.0_vip, seedance2.0fast_vip",
				`dreamina image2video --image=./first.png --prompt="camera push in"`,
			},
		},
		{
			args: []string{"help", "multiframe2video"},
			contains: []string{
				"Upload multiple local images, then submit a Dreamina intelligent multi-frame video task for coherent visual storytelling.",
				"repeat --transition-duration once per transition segment, or omit it to default each segment to 3 seconds",
				`dreamina multiframe2video --images ./a.png,./b.png,./c.png --transition-prompt="turn from A to B" --transition-prompt="turn from B to C"`,
			},
		},
		{
			args: []string{"help", "multimodal2video"},
			contains: []string{
				"supports all-around references, and supports the Seedance 2.0 family",
				"audio inputs must be 2-15 seconds",
				`dreamina multimodal2video --image ./input.png --video ./ref.mp4 --audio ./music.mp3 --model_version=seedance2.0fast --duration=5`,
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
				t.Fatalf("help output for %v missing %q: %q", tc.args, want, text)
			}
		}
	}
}

func TestGeneratorHelpAlignedForCommandHelpFlag(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"frames2video", "-h"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Upload two local images as first and last frames, then submit a Dreamina video generation task.") {
		t.Fatalf("unexpected frames2video help output: %q", text)
	}
	if !strings.Contains(text, "supported values by model: 3.0/3.5pro -> 720p or 1080p; seedance2.0 family -> 720p") {
		t.Fatalf("expected frames2video flag detail in help output: %q", text)
	}
	if !strings.Contains(text, `dreamina frames2video --first=./start.png --last=./end.png --prompt="season changes"`) {
		t.Fatalf("expected frames2video example in help output: %q", text)
	}
}

func TestValidateAuthTokenHelpAligned(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"help", "validate-auth-token"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Usage:\n  dreamina validate-auth-token [flags]") {
		t.Fatalf("unexpected validate-auth-token help output: %q", text)
	}
	if !strings.Contains(text, "Debug: validate the local credential with the backend") {
		t.Fatalf("missing validate-auth-token summary: %q", text)
	}
	if !strings.Contains(text, "Examples:\n  dreamina validate-auth-token") {
		t.Fatalf("missing validate-auth-token example: %q", text)
	}
}

func TestGlobalVersionFlagRunsVersionOutput(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"--version"})

	_, err := root.ExecuteC()
	if err != nil {
		t.Fatalf("ExecuteC failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"version"`) || !strings.Contains(text, `"commit"`) {
		t.Fatalf("unexpected version output: %q", text)
	}
}
