package app

import (
	"errors"

	authclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/auth"
	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
	mcpclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/mcp"
	resourceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/resource"
	"code.byted.org/videocut-aigc/dreamina_cli/components/gen"
	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
	"code.byted.org/videocut-aigc/dreamina_cli/components/task"
	"code.byted.org/videocut-aigc/dreamina_cli/config"
	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

type Clients struct {
	Auth     any
	Commerce any
	MCP      any
	Resource any
}

type AppContext struct {
	// 当前字段用途：
	// - loaded config
	// - login service
	// - generator registry/service
	// - Dreamina clients
	Config  any
	Clients Clients
	Login   any
	Tasks   any
	Gen     any
}

// NewContext 创建整个 CLI 运行上下文，统一装配配置、客户端、登录服务、任务存储和生成服务。
func NewContext() (*AppContext, error) {
	// 当前流程：
	// 1. config.Load()
	// 2. infra/httpclient.New() multiple times
	// 3. construct Dreamina-specific clients
	// 4. components/login.NewService(...)
	// 5. components/gen.DefaultRegistry()
	// 6. components/gen.RegisterDreaminaHandlers(...)
	// 7. assemble AppContext
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	http, err := httpclient.New()
	if err != nil {
		return nil, err
	}
	authCli := authclient.New(http)
	commerceCli := commerceclient.New(http)
	mcpCli := mcpclient.New(http)
	resourceCli := resourceclient.New(http)

	loginSvc, err := login.NewService()
	if err != nil {
		return nil, err
	}
	taskStore, err := task.NewStore()
	if err != nil {
		return nil, err
	}
	genSvc, err := gen.NewService(taskStore, mcpCli, resourceCli)
	if err != nil {
		return nil, err
	}

	return &AppContext{
		Config: cfg,
		Clients: Clients{
			Auth:     authCli,
			Commerce: commerceCli,
			MCP:      mcpCli,
			Resource: resourceCli,
		},
		Login: loginSvc,
		Tasks: taskStore,
		Gen:   genSvc,
	}, nil
}

// RequireLogin 确保当前上下文里的登录服务已经具备可用凭证。
func (a *AppContext) RequireLogin() error {
	if a == nil || a.Login == nil {
		return errors.New("login is not configured")
	}
	if svc, ok := a.Login.(*login.Service); ok {
		return svc.RequireUsableSession()
	}
	return nil
}

// CurrentSession 返回当前上下文里保存的登录会话服务对象。
func (a *AppContext) CurrentSession() any {
	if a == nil {
		return nil
	}
	return a.Login
}

// CurrentClientSession 返回当前上下文里装配好的客户端集合。
func (a *AppContext) CurrentClientSession() any {
	if a == nil {
		return nil
	}
	return a.Clients
}

// TaskStore 返回当前上下文里的任务存储实例。
func (a *AppContext) TaskStore() any {
	if a == nil {
		return nil
	}
	return a.Tasks
}

// GenService 返回当前上下文里的生成服务实例。
func (a *AppContext) GenService() any {
	if a == nil {
		return nil
	}
	return a.Gen
}
