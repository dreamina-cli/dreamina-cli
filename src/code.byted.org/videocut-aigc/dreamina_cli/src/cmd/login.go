package cmd

import (
	"fmt"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

// newLoginCommand 创建登录命令入口。
func newLoginCommand(app any) *Command {
	// login 命令支持 --headless 和 --debug 参数。
	return &Command{
		Use: "login",
		RunE: func(cmd *Command, args []string) error {
			opts, err := parseLoginRunOptions("login", args)
			if err != nil {
				return err
			}
			svc, err := login.NewService()
			if err != nil {
				return err
			}
			return svc.RunLogin(opts, cmd.OutOrStdout())
		},
	}
}

// newReloginCommand 创建重新登录命令入口。
func newReloginCommand(app any) *Command {
	// relogin 命令会清理本地凭证后重新走登录流程。
	return &Command{
		Use: "relogin",
		RunE: func(cmd *Command, args []string) error {
			opts, err := parseLoginRunOptions("relogin", args)
			if err != nil {
				return err
			}
			svc, err := login.NewService()
			if err != nil {
				return err
			}
			return svc.RunRelogin(opts, cmd.OutOrStdout())
		},
	}
}

// parseLoginRunOptions 解析登录命令支持的 headless/debug 参数。
func parseLoginRunOptions(use string, args []string) (login.RunOptions, error) {
	opts := login.RunOptions{Port: config.DefaultLoginCallbackPort}
	for _, arg := range args {
		switch arg {
		case "--headless":
			opts.Headless = true
		case "--debug":
			opts.Debug = true
		default:
			if strings.HasPrefix(arg, "-") {
				name := arg
				if idx := strings.Index(name, "="); idx >= 0 {
					name = name[:idx]
				}
				return login.RunOptions{}, fmt.Errorf("unknown flag: %s", name)
			}
			return login.RunOptions{}, fmt.Errorf("unknown command %q for %q", arg, "dreamina "+strings.TrimSpace(use))
		}
	}
	return opts, nil
}

// parsePositiveInt 解析一个只包含数字字符的正整数；遇到非法字符直接返回 0。
func parsePositiveInt(s string) int {
	n := 0
	for _, ch := range strings.TrimSpace(s) {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
