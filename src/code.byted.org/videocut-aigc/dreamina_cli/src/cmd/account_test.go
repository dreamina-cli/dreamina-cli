package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
)

func TestListTaskCommandUsesCookieSessionWithoutCredential(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg dir: %v", err)
	}
	cookieBody := []byte("{\n  \"cookie\": \"sid=test-cookie\",\n  \"uid\": \"u-cookie-only\"\n}\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "cookie.json"), cookieBody, 0o600); err != nil {
		t.Fatalf("write cookie session: %v", err)
	}
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"list_task"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("list_task should accept cookie-only session: %v", err)
	}
	if got := out.String(); got != "[]\n" {
		t.Fatalf("unexpected list_task output: %q", got)
	}
}

func TestListTaskCommandRejectsUnknownFlagBeforeLogin(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	root := NewRootCommand()
	root.SetArgs([]string{"list_task", "--badflag"})

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatalf("expected unknown flag error")
	}
	if got := err.Error(); got != "unknown flag: --badflag" {
		t.Fatalf("unexpected list_task badflag error: %q", got)
	}
}

func TestQueryResultCommandRejectsUnknownFlagBeforeLogin(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	root := NewRootCommand()
	root.SetArgs([]string{"query_result", "--badflag"})

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatalf("expected unknown flag error")
	}
	if got := err.Error(); got != "unknown flag: --badflag" {
		t.Fatalf("unexpected query_result badflag error: %q", got)
	}
}

func TestQueryResultCommandUsesCookieSessionWithoutCredential(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg dir: %v", err)
	}
	cookieBody := []byte("{\n  \"cookie\": \"sid=test-cookie\",\n  \"uid\": \"u-cookie-only\"\n}\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "cookie.json"), cookieBody, 0o600); err != nil {
		t.Fatalf("write cookie session: %v", err)
	}
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	root := NewRootCommand()
	var out bytes.Buffer
	root.out = &out
	root.SetArgs([]string{"query_result", "--submit_id=cookie-only-submit"})

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatalf("expected task-not-found error")
	}
	if got := err.Error(); got != `task "cookie-only-submit" not found` {
		t.Fatalf("unexpected query_result error: %q", got)
	}
	if strings.Contains(out.String(), "未检测到有效登录态") {
		t.Fatalf("query_result should not require credential when cookie.json exists: %q", out.String())
	}
}

func TestAccountCommandsRejectUnknownFlagBeforeExecution(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	for _, args := range [][]string{
		{"user_credit", "--badflag"},
		{"validate-auth-token", "--badflag"},
		{"import_login_response", "--badflag"},
	} {
		root := NewRootCommand()
		root.SetArgs(args)

		_, err := root.ExecuteC()
		if err == nil {
			t.Fatalf("expected unknown flag error for %v", args)
		}
		if got := err.Error(); got != "unknown flag: --badflag" {
			t.Fatalf("unexpected badflag error for %v: %q", args, got)
		}
	}
}

func TestCommandsRejectUnexpectedPositionalArgs(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"version", "foo"}, want: `unknown command "foo" for "dreamina version"`},
		{args: []string{"user_credit", "foo"}, want: `unknown command "foo" for "dreamina user_credit"`},
		{args: []string{"validate-auth-token", "foo"}, want: `unknown command "foo" for "dreamina validate-auth-token"`},
		{args: []string{"query_result", "foo"}, want: `unknown command "foo" for "dreamina query_result"`},
		{args: []string{"list_task", "foo"}, want: `unknown command "foo" for "dreamina list_task"`},
		{args: []string{"import_login_response", "foo"}, want: `unknown command "foo" for "dreamina import_login_response"`},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		root.SetArgs(tc.args)

		_, err := root.ExecuteC()
		if err == nil {
			t.Fatalf("expected positional-arg error for %v", tc.args)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected positional-arg error for %v: %q", tc.args, got)
		}
	}
}

func TestCommandsReportMissingFlagArguments(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"query_result", "--submit_id"}, want: `flag needs an argument: --submit_id`},
		{args: []string{"query_result", "--submit_id=abc", "--download_dir"}, want: `flag needs an argument: --download_dir`},
		{args: []string{"list_task", "--limit"}, want: `flag needs an argument: --limit`},
		{args: []string{"list_task", "--offset"}, want: `flag needs an argument: --offset`},
		{args: []string{"list_task", "--gen_status"}, want: `flag needs an argument: --gen_status`},
		{args: []string{"import_login_response", "--file"}, want: `flag needs an argument: --file`},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		root.SetArgs(tc.args)

		_, err := root.ExecuteC()
		if err == nil {
			t.Fatalf("expected missing-argument error for %v", tc.args)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected missing-argument error for %v: %q", tc.args, got)
		}
	}
}

func TestListTaskRejectsInvalidIntegerFlags(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"list_task", "--limit=abc"}, want: `invalid argument "abc" for "--limit" flag: strconv.ParseInt: parsing "abc": invalid syntax`},
		{args: []string{"list_task", "--offset=abc"}, want: `invalid argument "abc" for "--offset" flag: strconv.ParseInt: parsing "abc": invalid syntax`},
		{args: []string{"list_task", "--limit="}, want: `invalid argument "" for "--limit" flag: strconv.ParseInt: parsing "": invalid syntax`},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		root.SetArgs(tc.args)

		_, err := root.ExecuteC()
		if err == nil {
			t.Fatalf("expected invalid-int error for %v", tc.args)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected invalid-int error for %v: %q", tc.args, got)
		}
	}
}

func TestUnknownCommandSuggestsUnderscoreAlias(t *testing.T) {
	t.Helper()

	root := NewRootCommand()
	root.SetArgs([]string{"import-login-response", "--badflag"})

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	want := "unknown command \"import-login-response\" for \"dreamina\"\n\nDid you mean this?\n\timport_login_response\n"
	if got := err.Error(); got != want {
		t.Fatalf("unexpected suggestion error:\nwant=%q\ngot=%q", want, got)
	}
}

func TestWriteOriginalTaskNotFoundLogMatchesExpectedShape(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	writeOriginalTaskNotFoundLog(&out, "does-not-exist-123", time.Date(2026, 4, 5, 13, 45, 10, 0, time.Local), 87*time.Microsecond)

	want := "\r\n" +
		"2026/04/05 13:45:10 \x1b[31;1m/opt/tiger/compile_path/src/code.byted.org/videocut-aigc/dreamina_cli/components/task/store.go:278 \x1b[35;1mrecord not found\n" +
		"\x1b[0m\x1b[33m[0.087ms] \x1b[34;1m[rows:0]\x1b[0m SELECT * FROM `aigc_task` WHERE submit_id = \"does-not-exist-123\" ORDER BY `aigc_task`.`submit_id` LIMIT 1\n"
	if got := out.String(); got != want {
		t.Fatalf("unexpected not-found log:\nwant=%q\ngot=%q", want, got)
	}
}

func TestBuildUserCreditOutputMatchesOriginalPublicShape(t *testing.T) {
	t.Helper()

	got := buildUserCreditOutput(&commerceclient.UserCredit{
		CreditCount:    99,
		BenefitType:    "maestro",
		VIPCredit:      20,
		GiftCredit:     20,
		PurchaseCredit: 0,
		TotalCredit:    40,
	})
	if got.VIPCredit != 20 || got.GiftCredit != 20 || got.PurchaseCredit != 0 || got.TotalCredit != 40 {
		t.Fatalf("unexpected user_credit output: %#v", got)
	}
}

func TestBuildQueryResultMCPSessionPreservesCookieHeadersAndUID(t *testing.T) {
	t.Helper()

	session := buildQueryResultMCPSession(map[string]any{
		"cookie": "sid=test-cookie",
		"headers": map[string]any{
			"User-Agent": "ua-test",
			"X-Test":     "1",
		},
		"uid": "user-1",
	})
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.Cookie != "sid=test-cookie" {
		t.Fatalf("unexpected session cookie: %#v", session.Cookie)
	}
	if session.Headers["User-Agent"] != "ua-test" || session.Headers["X-Test"] != "1" {
		t.Fatalf("unexpected session headers: %#v", session.Headers)
	}
	if session.UserID != "user-1" {
		t.Fatalf("unexpected session user id: %#v", session.UserID)
	}
}

func TestQueryResultMatchedRawItemReturnsRawMatchedBySubmitID(t *testing.T) {
	t.Helper()

	resp := &mcpclient.GetHistoryByIdsResponse{
		Items: map[string]*mcpclient.HistoryItem{
			"other": {
				SubmitID: "other",
				Raw:      map[string]any{"submit_id": "other"},
			},
			"record-1": {
				SubmitID:        "submit-1",
				HistoryID:       "hist-1",
				HistoryRecordID: "record-1",
				Raw: map[string]any{
					"submit_id":         "submit-1",
					"history_record_id": "record-1",
					"prompt":            "raw value",
				},
			},
		},
	}

	got, ok := queryResultMatchedRawItem(resp, "submit-1").(map[string]any)
	if !ok {
		t.Fatalf("expected raw map, got %T", queryResultMatchedRawItem(resp, "submit-1"))
	}
	if got["prompt"] != "raw value" {
		t.Fatalf("unexpected raw item: %#v", got)
	}
}

func TestGeneratorCommandsReportOriginalRequiredFlagMessagesBeforeLogin(t *testing.T) {
	t.Helper()

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"image_upscale"}, want: `required flag(s) "image", "resolution_type" not set`},
		{args: []string{"image_upscale", "--resolution_type=2k"}, want: `required flag(s) "image" not set`},
		{args: []string{"image2video", "--prompt=x"}, want: `required flag(s) "image" not set`},
		{args: []string{"frames2video", "--last=/tmp/end.png", "--prompt=x"}, want: `required flag(s) "first" not set`},
		{args: []string{"frames2video", "--prompt=x"}, want: `required flag(s) "first", "last" not set`},
		{args: []string{"image2image", "--prompt=x"}, want: `required flag(s) "images" not set`},
		{args: []string{"multiframe2video", "--prompt=x"}, want: `required flag(s) "images" not set`},
	}

	for _, tc := range cases {
		root := NewRootCommand()
		root.SetArgs(tc.args)

		_, err := root.ExecuteC()
		if err == nil {
			t.Fatalf("expected error for args %v", tc.args)
		}
		if got := err.Error(); got != tc.want {
			t.Fatalf("unexpected error for %v: %q", tc.args, got)
		}
	}
}

func TestMultimodal2VideoRequiresLoginBeforeInputValidation(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	root := NewRootCommand()
	root.SetArgs([]string{"multimodal2video"})

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatalf("expected login-required error")
	}
	if got := err.Error(); got != "未检测到有效 cookie 会话，请先准备 ~/.dreamina_cli/cookie.json" {
		t.Fatalf("unexpected multimodal2video error: %q", got)
	}
}
