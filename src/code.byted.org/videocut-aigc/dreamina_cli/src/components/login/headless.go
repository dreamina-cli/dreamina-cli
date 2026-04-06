package login

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type Clock interface {
	Now() any
	Sleep(v ...any)
}

type realClock struct{}

func (c *realClock) Now() any { return time.Now() }

func (c *realClock) Sleep(v ...any) {
	delay := 200 * time.Millisecond
	for _, arg := range v {
		if d, ok := arg.(time.Duration); ok && d > 0 {
			delay = d
			break
		}
	}
	time.Sleep(delay)
}

type HeadlessLoginRunner interface {
	Start(ctx context.Context, url string) error
}

type headlessBrowserRunner struct{}

func (r *headlessBrowserRunner) Start(ctx context.Context, url string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	chromePath, err := findHeadlessChrome(runtime.GOOS)
	if err != nil {
		return fmt.Errorf("headless login requires Google Chrome or Chromium; open this URL manually: %s", url)
	}
	flow, err := resolveHeadlessFlowTargets(url)
	if err != nil {
		return err
	}
	userDataDir, err := os.MkdirTemp("", "dreamina-headless-*")
	if err != nil {
		return fmt.Errorf("chrome failed to start: create temp profile: %w", err)
	}
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(
		ctx,
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("enable-automation", true),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("enable-features", "AutomationControlled"),
		chromedp.Flag("remote-debugging-port", 0),
		chromedp.Flag("user-data-dir", userDataDir),
	)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	cleanup := func() {
		cancelBrowser()
		cancelAllocator()
		_ = os.RemoveAll(userDataDir)
	}
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	var qrOnce sync.Once
	var loginOnce sync.Once
	chromedp.ListenTarget(browserCtx, func(event any) {
		ev, ok := event.(*network.EventResponseReceived)
		if !ok || ev == nil || ev.Response == nil {
			return
		}
		responseURL := strings.TrimSpace(ev.Response.URL)
		switch {
		case matchesHeadlessResponseURL(responseURL, "/oauth/get_qrcode"):
			go qrOnce.Do(func() {
				if err := handleHeadlessQRCodeResponse(browserCtx, ev.RequestID, flow.ManualImportURL); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "[dreamina headless] qrcode capture failed: %v\n", err)
				}
			})
		case matchesHeadlessResponseURL(responseURL, "/dreamina/cli/v1/dreamina_cli_login"):
			go loginOnce.Do(func() {
				if err := handleHeadlessLoginResponse(browserCtx, ev.RequestID, flow.CallbackURL); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "[dreamina headless] login callback failed: %v\n", err)
				}
			})
		}
	})

	err = chromedp.Run(
		browserCtx,
		network.Enable().
			WithMaxTotalBufferSize(32*1024*1024).
			WithMaxResourceBufferSize(8*1024*1024).
			WithMaxPostDataSize(1024*1024),
		chromedp.Navigate(flow.LoginURL),
	)
	if err != nil {
		cleanup()
		return fmt.Errorf("chrome failed to start: %w", err)
	}
	return nil
}

func readLoginDataResponse(v ...any) ([]byte, error) {
	// 当前行为：
	// - invoke network.GetResponseBody(...).Do(...)
	// - retry up to 5 times when the body is unreadable or empty
	// - sleep between attempts
	// - do not JSON-decode; raw body is returned upward
	attempts := 5
	sleep := 200 * time.Millisecond
	for _, arg := range v {
		switch value := arg.(type) {
		case int:
			if value > 0 {
				attempts = value
			}
		case time.Duration:
			if value > 0 {
				sleep = value
			}
		}
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		body, err := extractHeadlessBody(v...)
		if err == nil && len(strings.TrimSpace(string(body))) > 0 {
			return body, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("headless response body is empty")
		}
		if attempt+1 < attempts {
			time.Sleep(sleep)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("headless response capture is not restored")
	}
	return nil, lastErr
}

func readQRCodeResponse(v ...any) (*headlessQRCodeResponse, error) {
	// 当前行为：
	// - invoke network.GetResponseBody(...).Do(...)
	// - retry up to 10 times when body is unreadable, empty, or invalid JSON
	// - sleep between attempts
	// - decode into headlessQRCodeResponse{Qrcode, Token}
	attempts := 10
	sleep := 200 * time.Millisecond
	for _, arg := range v {
		switch value := arg.(type) {
		case int:
			if value > 0 {
				attempts = value
			}
		case time.Duration:
			if value > 0 {
				sleep = value
			}
		}
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		body, err := extractHeadlessBody(v...)
		if err == nil {
			resp := &headlessQRCodeResponse{}
			if json.Unmarshal(body, resp) == nil && strings.TrimSpace(resp.Qrcode) != "" {
				return resp, nil
			}
			lastErr = fmt.Errorf("decode qrcode response: invalid or empty qrcode payload")
		} else {
			lastErr = err
		}
		if attempt+1 < attempts {
			time.Sleep(sleep)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("headless qrcode capture is not restored")
	}
	return nil, lastErr
}

func findHeadlessChrome(osName string) (string, error) {
	candidates := headlessChromeCandidates(osName)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil && strings.TrimSpace(resolved) != "" {
			return resolved, nil
		}
		if fileInfo, err := os.Stat(candidate); err == nil && !fileInfo.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("chrome executable not found")
}

func headlessChromeCandidates(osName string) []string {
	switch osName {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"google-chrome",
			"chromium",
		}
	case "linux":
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
	case "windows":
		return []string{
			"chrome.exe",
		}
	default:
		return []string{
			"google-chrome",
			"chromium",
		}
	}
}

func extractHeadlessBody(v ...any) ([]byte, error) {
	for _, arg := range v {
		switch value := arg.(type) {
		case []byte:
			body := append([]byte(nil), value...)
			if len(body) > 0 {
				return body, nil
			}
		case string:
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				return []byte(trimmed), nil
			}
			if body, err := os.ReadFile(trimmed); err == nil {
				return body, nil
			}
			return []byte(trimmed), nil
		case io.Reader:
			body, err := io.ReadAll(value)
			if err != nil {
				return nil, err
			}
			if len(body) == 0 {
				return nil, fmt.Errorf("headless response body is empty")
			}
			return body, nil
		case *headlessQRCodeResponse:
			if value == nil {
				continue
			}
			body, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			return body, nil
		case map[string]any:
			body, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			return body, nil
		case func() ([]byte, error):
			return value()
		case func() (string, error):
			text, err := value()
			if err != nil {
				return nil, err
			}
			return []byte(text), nil
		}
	}
	return nil, fmt.Errorf("headless response body is unavailable")
}

type headlessFlowTargets struct {
	LoginURL        string
	CallbackURL     string
	ManualImportURL string
}

func resolveHeadlessFlowTargets(raw string) (*headlessFlowTargets, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("headless login url is empty")
	}
	outerURL, err := parseURL(raw)
	if err != nil {
		return nil, fmt.Errorf("parse headless login url: %w", err)
	}
	authURL := strings.TrimSpace(outerURL.Query().Get("auth_url"))
	if authURL == "" {
		authURL = raw
	}
	authParsed, err := parseURL(authURL)
	if err != nil {
		return nil, fmt.Errorf("parse authorization url: %w", err)
	}
	callbackURL := strings.TrimSpace(authParsed.Query().Get("callback"))
	if callbackURL == "" {
		callbackURL = strings.TrimSpace(authParsed.Query().Get("redirect_uri"))
	}
	secretKey := strings.TrimSpace(authParsed.Query().Get("random_secret_key"))
	aid := strings.TrimSpace(authParsed.Query().Get("aid"))
	if aid == "" {
		aid = "513695"
	}
	manualImportURL := ""
	if authParsed.Scheme != "" && authParsed.Host != "" && secretKey != "" {
		manualURL, err := parseURL(fmt.Sprintf("%s://%s/dreamina/cli/v1/dreamina_cli_login", authParsed.Scheme, authParsed.Host))
		if err == nil {
			query := manualURL.Query()
			query.Set("aid", aid)
			query.Set("random_secret_key", secretKey)
			query.Set("web_version", "7.5.0")
			manualURL.RawQuery = query.Encode()
			manualImportURL = manualURL.String()
		}
	}
	return &headlessFlowTargets{
		LoginURL:        authURL,
		CallbackURL:     callbackURL,
		ManualImportURL: manualImportURL,
	}, nil
}

func matchesHeadlessResponseURL(raw string, pathSuffix string) bool {
	raw = strings.TrimSpace(raw)
	pathSuffix = strings.TrimSpace(pathSuffix)
	if raw == "" || pathSuffix == "" {
		return false
	}
	if strings.Contains(raw, pathSuffix) {
		return true
	}
	u, err := parseURL(raw)
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(u.Path), pathSuffix)
}

func handleHeadlessQRCodeResponse(ctx context.Context, requestID network.RequestID, manualImportURL string) error {
	body, err := readQRCodeResponse(func() ([]byte, error) {
		return network.GetResponseBody(requestID).Do(ctx)
	})
	if err != nil {
		return err
	}
	pngPath, pathErr := saveQRCodePNGBase64(body.Qrcode)
	termQR, renderErr := renderQRCodeTerminalBase64(body.Qrcode)
	if pathErr != nil && renderErr != nil {
		return pathErr
	}
	printHeadlessQRCode(termQR, pngPath, body.Token, manualImportURL)
	return nil
}

func handleHeadlessLoginResponse(ctx context.Context, requestID network.RequestID, callbackURL string) error {
	if strings.TrimSpace(callbackURL) == "" {
		return fmt.Errorf("headless callback url is empty")
	}
	body, err := readLoginDataResponse(func() ([]byte, error) {
		return network.GetResponseBody(requestID).Do(ctx)
	})
	if err != nil {
		return err
	}
	return postHeadlessLoginCallback(ctx, callbackURL, body)
}

func postHeadlessLoginCallback(ctx context.Context, callbackURL string, body []byte) error {
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return fmt.Errorf("callback url is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytesNewReader(body))
	if err != nil {
		return fmt.Errorf("create callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("post callback response: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback response status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
