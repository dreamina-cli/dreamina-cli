package login

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"
)

func TestAuthorizationURLMatchesOriginalBrowserFlow(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)
	raw, err := mgr.AuthorizationURL(60713)
	if err != nil {
		t.Fatalf("AuthorizationURL failed: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse AuthorizationURL: %v", err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://jimeng.jianying.com/ai-tool/login" {
		t.Fatalf("unexpected authorization endpoint: %q", got)
	}
	query := u.Query()
	if query.Get("callback") != "http://127.0.0.1:60713/dreamina/callback/save_session" {
		t.Fatalf("unexpected callback query: %q", query.Get("callback"))
	}
	if query.Get("from") != "cli" {
		t.Fatalf("unexpected from query: %q", query.Get("from"))
	}
	if query.Get("random_secret_key") == "" {
		t.Fatalf("expected random_secret_key in query: %q", raw)
	}
	if query.Get("redirect_uri") != "" {
		t.Fatalf("did not expect redirect_uri in query: %q", raw)
	}
	if query.Get("aid") != "" {
		t.Fatalf("did not expect aid in query: %q", raw)
	}
}

func TestHeadlessAuthorizationURLUsesBrowserLoginURL(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)
	authURL, err := mgr.AuthorizationURL(60713)
	if err != nil {
		t.Fatalf("AuthorizationURL failed: %v", err)
	}
	headlessURL, err := mgr.HeadlessAuthorizationURL(60713)
	if err != nil {
		t.Fatalf("HeadlessAuthorizationURL failed: %v", err)
	}
	if headlessURL != authURL {
		t.Fatalf("unexpected headless authorization url: %q != %q", headlessURL, authURL)
	}
}

func TestManualImportURLMatchesOriginalQueryShape(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)
	raw, err := mgr.ManualImportURL()
	if err != nil {
		t.Fatalf("ManualImportURL failed: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse ManualImportURL: %v", err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://jimeng.jianying.com/dreamina/cli/v1/dreamina_cli_login" {
		t.Fatalf("unexpected manual import endpoint: %q", got)
	}
	query := u.Query()
	if query.Get("aid") != "513695" {
		t.Fatalf("unexpected aid query: %q", query.Get("aid"))
	}
	if query.Get("random_secret_key") == "" {
		t.Fatalf("expected random_secret_key in query: %q", raw)
	}
	if query.Get("web_version") != "7.5.0" {
		t.Fatalf("unexpected web_version query: %q", query.Get("web_version"))
	}
	if query.Get("platform_app_id") != "" {
		t.Fatalf("did not expect platform_app_id in query: %q", raw)
	}
}

func TestLoginGuideURLMatchesOriginalGuidePage(t *testing.T) {
	t.Helper()

	mgr := newURLTestLoginManager(t)
	raw, err := mgr.LoginGuideURL()
	if err != nil {
		t.Fatalf("LoginGuideURL failed: %v", err)
	}
	if raw != "https://jimeng.jianying.com/ai-tool/login" {
		t.Fatalf("unexpected login guide url: %q", raw)
	}
}

func newURLTestLoginManager(t *testing.T) *Manager {
	t.Helper()

	dir := t.TempDir()
	return &Manager{
		dir:            dir,
		credentialPath: filepath.Join(dir, "credential.json"),
		loginBaseURL:   "https://jimeng.jianying.com/ai-tool/login",
	}
}

func TestFormatSessionPayloadMatchesOriginalVisibleFields(t *testing.T) {
	payload := map[string]any{
		"cookie": "sid=test; ttwid=value",
		"headers": map[string]any{
			"Accept":             "application/json, text/plain, */*",
			"Accept-Encoding":    "gzip, deflate, br, zstd",
			"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
			"Appvr":              "8.4.0",
			"Cookie":             "sid=hidden",
			"Device-Time":        "1775340245",
			"Host":               "jimeng.jianying.com",
			"Lan":                "zh-Hans",
			"Pf":                 "7",
			"Priority":           "u=1, i",
			"Referer":            "https://jimeng.jianying.com/ai-tool/login",
			"Sec-Ch-Ua":          "\"Chromium\";v=\"146\"",
			"Sec-Ch-Ua-Mobile":   "?0",
			"Sec-Ch-Ua-Platform": "\"Windows\"",
			"Sec-Fetch-Dest":     "empty",
			"Sec-Fetch-Mode":     "cors",
			"Sec-Fetch-Site":     "same-origin",
			"Sign":               "313a4b30f83845c6669a6f89098373b0",
			"Sign-Ver":           "1",
			"User-Agent":         "Mozilla/5.0",
			"X-Request-Id":       "hidden-request-id",
		},
		"uid":     json.Number("2948212243832628"),
		"user_id": "2948212243832628",
	}

	got := FormatSessionPayload(payload)
	want := `{
  "cookie": "sid=test; ttwid=value",
  "headers": {
    "Accept": "application/json, text/plain, */*",
    "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    "Appvr": "8.4.0",
    "Device-Time": "1775340245",
    "Lan": "zh-Hans",
    "Pf": "7",
    "Priority": "u=1, i",
    "Referer": "https://jimeng.jianying.com/ai-tool/login",
    "Sec-Ch-Ua": "\"Chromium\";v=\"146\"",
    "Sec-Ch-Ua-Mobile": "?0",
    "Sec-Ch-Ua-Platform": "\"Windows\"",
    "Sec-Fetch-Dest": "empty",
    "Sec-Fetch-Mode": "cors",
    "Sec-Fetch-Site": "same-origin",
    "Sign": "313a4b30f83845c6669a6f89098373b0",
    "Sign-Ver": "1",
    "User-Agent": "Mozilla/5.0"
  },
  "uid": 2948212243832628
}`
	if got != want {
		t.Fatalf("unexpected session payload:\n%s", got)
	}
}
