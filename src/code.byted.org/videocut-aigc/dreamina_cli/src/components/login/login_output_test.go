package login

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type fakeServerStarter struct {
	start func(v ...any) (ServerInstance, error)
}

func (f fakeServerStarter) Start(v ...any) (ServerInstance, error) {
	return f.start(v...)
}

type fakeServerInstance struct {
	port int
}

func (f fakeServerInstance) Port() int { return f.port }

func (f fakeServerInstance) Shutdown(ctx context.Context) error { return nil }

func TestRunBrowserLoginPrintsOriginalBrowserHintWithoutFallbackInstructions(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	mgr, err := New()
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	svc := &Service{
		mgr: mgr,
		browserOpener: BrowserOpenerFunc(func(url string) error {
			return mgr.markLoginCompleted()
		}),
		serverStarter: fakeServerStarter{
			start: func(v ...any) (ServerInstance, error) {
				return fakeServerInstance{port: 60713}, nil
			},
		},
	}

	var out bytes.Buffer
	if err := svc.runBrowserLogin(RunOptions{Port: 0}, &out); err != nil {
		t.Fatalf("runBrowserLogin failed: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "已尝试打开默认浏览器，请在页面中完成 Dreamina 登录授权。") {
		t.Fatalf("missing original browser hint: %q", text)
	}
	if strings.Contains(text, "请在浏览器中打开以下链接") {
		t.Fatalf("did not expect fallback instructions after successful browser open: %q", text)
	}
	if strings.Contains(text, "如果需要 agent 手动导入登录态") {
		t.Fatalf("did not expect manual import instructions after successful browser open: %q", text)
	}
	if strings.Contains(text, "Dreamina 登录成功") {
		t.Fatalf("did not expect extra success banner: %q", text)
	}
	if strings.Contains(text, "[DREAMINA:LOGIN_SUCCESS]") {
		t.Fatalf("did not expect login success tag by default: %q", text)
	}
}

func TestRunBrowserLoginPrintsFallbackInstructionsWhenBrowserOpenFails(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	mgr, err := New()
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	svc := &Service{
		mgr: mgr,
		browserOpener: BrowserOpenerFunc(func(url string) error {
			_ = mgr.setLoginFailure(context.DeadlineExceeded)
			return context.DeadlineExceeded
		}),
		serverStarter: fakeServerStarter{
			start: func(v ...any) (ServerInstance, error) {
				return fakeServerInstance{port: 60713}, nil
			},
		},
	}

	var out bytes.Buffer
	err = svc.runBrowserLogin(RunOptions{Port: 0}, &out)
	if err == nil {
		t.Fatalf("expected runBrowserLogin failure when browser open fails and no callback arrives")
	}
	text := out.String()
	if !strings.Contains(text, "自动打开浏览器失败，请手动继续登录") {
		t.Fatalf("missing browser-open failure hint: %q", text)
	}
	if !strings.Contains(text, "请在浏览器中打开以下链接") {
		t.Fatalf("expected fallback authorization instructions: %q", text)
	}
	if !strings.Contains(text, "如果需要 agent 手动导入登录态") {
		t.Fatalf("expected manual import instructions: %q", text)
	}
}

func TestRunBrowserLoginDebugOutputMatchesOriginalStyle(t *testing.T) {
	t.Helper()

	cfgDir := filepath.Join(t.TempDir(), ".dreamina_cli")
	t.Setenv("DREAMINA_CONFIG_DIR", cfgDir)

	mgr, err := New()
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	svc := &Service{
		mgr: mgr,
		browserOpener: BrowserOpenerFunc(func(url string) error {
			_ = mgr.setLoginFailure(context.DeadlineExceeded)
			return context.DeadlineExceeded
		}),
		serverStarter: fakeServerStarter{
			start: func(v ...any) (ServerInstance, error) {
				return fakeServerInstance{port: 60713}, nil
			},
		},
	}

	var out bytes.Buffer
	err = svc.runBrowserLogin(RunOptions{Port: 60713, Debug: true}, &out)
	if err == nil {
		t.Fatalf("expected runBrowserLogin timeout in debug mode without callback")
	}

	text := out.String()
	if !strings.Contains(text, "https://jimeng.jianying.com/ai-tool/login?callback=http%3A%2F%2F127.0.0.1%3A60713%2Fdreamina%2Fcallback%2Fsave_session&from=cli&random_secret_key=") {
		t.Fatalf("expected fixed callback port in debug authorization url: %q", text)
	}
	if !strings.Contains(text, "[debug] 该接口返回 /dreamina/cli/v1/dreamina_cli_login 的完整响应 body。") {
		t.Fatalf("expected original debug hint: %q", text)
	}
	if strings.Contains(text, "Debug / remote guidance") {
		t.Fatalf("did not expect restored english debug guidance: %q", text)
	}
	if strings.Contains(text, "已尝试打开默认浏览器") {
		t.Fatalf("did not expect browser success hint in debug mode: %q", text)
	}
}
