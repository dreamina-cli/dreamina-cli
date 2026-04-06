package login

import (
	"context"
	"fmt"
	"io"
	"os"

	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	"code.byted.org/videocut-aigc/dreamina_cli/server"
)

type Service struct {
	// Service 负责登录管理、浏览器拉起、无头登录和本地回调服务协作。
	mgr             *Manager
	browserOpener   BrowserOpener
	headlessRunner  HeadlessLoginRunner
	clock           Clock
	serverStarter   ServerStarter
	userInfoProbe   UserInfoProbe
	userCreditProbe UserCreditProbe
}

type UserInfoProbe func(context.Context, any) (*commerceclient.UserInfo, error)

type UserCreditProbe func(context.Context, any) (*commerceclient.UserCredit, error)

func NewService(v ...any) (*Service, error) {
	// NewService 初始化登录服务，并补齐浏览器、无头模式和回调服务默认实现。
	mgr, err := New(v...)
	if err != nil {
		return nil, err
	}
	service := &Service{
		mgr:            mgr,
		browserOpener:  BrowserOpenerFunc(openBrowser),
		headlessRunner: &headlessBrowserRunner{},
		clock:          &realClock{},
		serverStarter:  &defaultServerStarter{},
	}
	for _, arg := range v {
		switch value := arg.(type) {
		case UserInfoProbe:
			service.userInfoProbe = value
		case UserCreditProbe:
			service.userCreditProbe = value
		}
	}
	return service, nil
}

func (s *Service) RequireUsableCredential() error {
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}
	return s.mgr.RequireUsableCredential()
}

func (s *Service) ParseAuthToken(v ...any) (any, error) {
	if s == nil || s.mgr == nil {
		return nil, fmt.Errorf("login manager is not initialized")
	}
	return s.mgr.ParseAuthToken(v...)
}

func (s *Service) ValidateAuthToken(v ...any) error {
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}
	return s.mgr.ValidateAuthToken(v...)
}

func (s *Service) Logout() error {
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}
	return s.mgr.ClearCredential()
}

func (s *Service) ImportLoginResponse(v ...any) error {
	// 手动导入登录响应后，会立即校验凭证并输出登录成功结果。
	if s == nil || s.mgr == nil {
		return fmt.Errorf("login manager is not initialized")
	}

	var (
		raw []byte
		out io.Writer = os.Stdout
	)

	for _, arg := range v {
		switch value := arg.(type) {
		case []byte:
			raw = value
		case io.Writer:
			out = value
		}
	}

	if len(raw) == 0 {
		return fmt.Errorf("login response body is empty")
	}

	if err := s.mgr.ImportLoginResponseJSON(raw); err != nil {
		return err
	}

	summary, _ := s.fetchAccountSummary(v...)
	printLoginSuccess(out, summary)
	printAccountSummary(out, summary)
	printLoginStateTag(out, "LOGIN_SUCCESS")
	return nil
}

type ServerStarter interface {
	Start(v ...any) (ServerInstance, error)
}

type ServerInstance interface {
	Port() int
	Shutdown(ctx context.Context) error
}

type defaultServerStarter struct{}

func (s *defaultServerStarter) Start(v ...any) (ServerInstance, error) {
	// 这里只做一层薄适配，便于登录流程注入测试替身。
	routes := []server.Route{}
	port := 0
	for _, arg := range v {
		switch value := arg.(type) {
		case []server.Route:
			routes = value
		case int:
			port = value
		}
	}
	return server.Start(routes, port)
}
