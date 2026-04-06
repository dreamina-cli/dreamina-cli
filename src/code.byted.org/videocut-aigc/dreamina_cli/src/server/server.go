package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

type Route struct {
	// Confirmed from newMux/type equality:
	// - first field is a string pattern
	// - second field is an interface-compatible handler
	Pattern string
	Handler any
}

type Instance struct {
	Listener net.Listener
	Server   *http.Server
	ErrCh    chan error
}

// Start 启动本地 HTTP 服务实例，并把后台 Serve 错误通过 ErrCh 回传。
func Start(routes []Route, port int) (*Instance, error) {
	// 当前行为：
	// - addr := fmt.Sprintf(":%d", port)
	// - ln, err := net.Listen("tcp", addr)
	// - if err != nil: return wrapped error
	// - mux := newMux(routes...)
	// - srv := &http.Server{Handler: mux}
	// - errCh := make(chan error, 1)
	// - spawn goroutine:
	//     serveErr := srv.Serve(ln)
	//     if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
	//         errCh <- serveErr
	//     }
	//     close(errCh)
	// - return &Instance{Listener: ln, Server: srv, ErrCh: errCh}, nil
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen callback server %s: %w", addr, err)
	}
	mux := newMux(routes...).(*http.ServeMux)
	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		serveErr := srv.Serve(ln)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()
	return &Instance{Listener: ln, Server: srv, ErrCh: errCh}, nil
}

// Port 返回当前服务监听到的实际 TCP 端口。
func (i *Instance) Port() int {
	// 当前行为：
	// - addr := i.Listener.Addr()
	// - tcpAddr := addr.(*net.TCPAddr)
	// - return tcpAddr.Port
	if i == nil || i.Listener == nil {
		return 0
	}
	addr, ok := i.Listener.Addr().(*net.TCPAddr)
	if !ok || addr == nil {
		return 0
	}
	return addr.Port
}

// Shutdown 优雅关闭服务，并在需要时返回后台 Serve 期间产生的错误。
func (i *Instance) Shutdown(ctx context.Context) error {
	// 当前行为：
	// - err := i.Server.Shutdown(ctx)
	// - if err != nil: return err
	// - serveErr, ok := <-i.ErrCh
	// - if ok && serveErr != nil: return serveErr
	// - return nil
	if i == nil || i.Server == nil {
		return nil
	}
	if err := i.Server.Shutdown(ctx); err != nil {
		return err
	}
	if i.ErrCh == nil {
		return nil
	}
	if serveErr, ok := <-i.ErrCh; ok && serveErr != nil {
		return serveErr
	}
	return nil
}

// newMux 根据给定路由集合构造 http.ServeMux。
func newMux(routes ...Route) any {
	// 当前行为：
	// - mux := new(http.ServeMux)
	// - for _, route := range routes {
	//     mux.Handle(route.Pattern, route.Handler)
	//   }
	// - return mux
	mux := http.NewServeMux()
	for _, route := range routes {
		handler, ok := route.Handler.(http.Handler)
		if !ok || handler == nil {
			continue
		}
		mux.Handle(route.Pattern, handler)
	}
	return mux
}
