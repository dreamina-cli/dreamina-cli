package login

import (
	"os/exec"
	"runtime"
	"strings"
)

type BrowserOpener interface {
	Open(url string) error
}

type BrowserOpenerFunc func(string) error

func (f BrowserOpenerFunc) Open(url string) error { return f(url) }

type browserLaunch struct {
	Name string
	Args []string
}

func openBrowser(url string) error {
	cmd, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}

	if err := exec.Command(cmd.Name, cmd.Args...).Start(); err != nil {
		return fmtErrorf("start browser command %q: %w", cmd.Name, err)
	}

	return nil
}

func browserCommand(osName string, url string) (*browserLaunch, error) {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return nil, fmtErrorf("browser url is empty")
	}

	switch osName {
	case "linux":
		return &browserLaunch{Name: "xdg-open", Args: []string{trimmed}}, nil
	case "darwin":
		return &browserLaunch{Name: "open", Args: []string{trimmed}}, nil
	case "windows":
		return &browserLaunch{
			Name: "rundll32",
			Args: []string{"url.dll,FileProtocolHandler", trimmed},
		}, nil
	default:
		return nil, fmtErrorf("unsupported browser platform %q", osName)
	}
}
