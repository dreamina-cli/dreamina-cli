package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type Command struct {
	Use      string
	RunE     commandRunner
	Children []*Command
	args     []string
	ctx      any
	in       any
	out      any
}

// ExecuteArgs 用给定参数执行根命令，并在执行前补齐上下文、输入输出和调试追踪信息。
func ExecuteArgs(args []string) error {
	// 执行入口会补齐上下文和输入输出对象，
	// 再按参数推断命令路径并在需要时输出调试追踪信息。
	root := NewRootCommand()
	root.SetArgs(args)
	ctx, ioState := ensureCommandContext(root)
	root.ctx = ctx
	if state, ok := ioState.(commandIO); ok {
		root.in = state.in
		root.out = state.out
	}
	withCommandTrace(root, guessedCommandPath(args), args)
	_, err := root.ExecuteC()
	return err
}

// NewRootCommand 构造 CLI 根命令，并挂载所有一级子命令。
func NewRootCommand() *Command {
	// 根命令挂载登录、任务查询、账号信息和全部生成类子命令。
	root := &Command{Use: "dreamina"}
	root.AddCommand(
		newHelpCommand(root),
		newCompletionCommand(),
		newCompleteCommand(),
		newLoginCommand(nil),
		newReloginCommand(nil),
		newImportLoginResponseCommand(nil),
		newSetCookieCommand(nil),
		newQueryResultCommand(nil),
		newListTaskCommand(nil),
		newUserCreditCommand(nil),
		newLogoutCommand(nil),
		newValidateAuthCommand(nil),
		newVersionCommand(),
	)
	addGeneratorCommands(root, nil)
	return root
}

// addGeneratorCommands 把所有生成类命令统一挂到根命令下。
func addGeneratorCommands(root *Command, app any) {
	// 所有生成类命令统一在这里挂到根命令下，具体参数解析见 generators.go。
	root.AddCommand(
		newText2VideoCommand(app),
		newImage2VideoCommand(app),
		newFrames2VideoCommand(app),
		newMultiFrame2VideoCommandWithUse(app, "multiframe2video"),
		newMultiFrame2VideoCommandWithUse(app, "ref2video"),
		newMultiModal2VideoCommand(app),
		newText2ImageCommand(app),
		newImage2ImageCommand(app),
		newImageUpscaleCommand(app),
	)
}

type commandIO struct {
	in  io.Reader
	out io.Writer
}

// ensureCommandContext 为命令执行补默认 context、stdin 和 stdout。
func ensureCommandContext(v ...any) (any, any) {
	ctx := context.Background()
	ioState := commandIO{in: os.Stdin, out: os.Stdout}
	for _, arg := range v {
		if cmd, ok := arg.(*Command); ok && cmd != nil {
			if existing, ok := cmd.ctx.(context.Context); ok {
				ctx = existing
			}
			if reader, ok := cmd.in.(io.Reader); ok {
				ioState.in = reader
			}
			if writer, ok := cmd.out.(io.Writer); ok {
				ioState.out = writer
			}
		}
	}
	return ctx, ioState
}

// guessedCommandPath 从原始参数里推断一个简短命令路径，供 trace 输出使用。
func guessedCommandPath(args []string) string {
	path := make([]string, 0, 2)
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.HasPrefix(arg, "--") {
			continue
		}
		path = append(path, arg)
		if len(path) == 2 {
			break
		}
	}
	return strings.Join(path, " ")
}

// withCommandTrace 在调试环境变量开启时，把命令路径和参数打印到 stderr。
func withCommandTrace(v ...any) any {
	traceEnabled := false
	for _, key := range []string{"DREAMINA_TRACE", "DREAMINA_DEBUG"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if value == "1" || value == "true" || value == "yes" {
			traceEnabled = true
			break
		}
	}
	if !traceEnabled {
		return nil
	}
	var (
		path string
		args []string
	)
	for _, arg := range v {
		switch value := arg.(type) {
		case string:
			if path == "" {
				path = strings.TrimSpace(value)
			}
		case []string:
			args = append([]string(nil), value...)
		}
	}
	if path == "" {
		path = "<root>"
	}
	_, _ = fmt.Fprintf(os.Stderr, "[DREAMINA:TRACE] command=%s args=%q\n", path, args)
	return nil
}

// requiresComplianceConfirmation 根据失败原因里的关键词判断是否需要额外合规确认提示。
func requiresComplianceConfirmation(failReason string) bool {
	// 根据失败原因中的关键词，判断是否需要额外提示用户做合规确认。
	failReason = strings.ToLower(strings.TrimSpace(failReason))
	if failReason == "" {
		return false
	}
	for _, token := range []string{
		"compliance",
		"safety",
		"policy",
		"forbidden",
		"violation",
		"审核",
		"风控",
		"敏感",
	} {
		if strings.Contains(failReason, token) {
			return true
		}
	}
	return false
}

// commandUsage 生成当前命令节点下可用子命令的简短帮助文本。
func commandUsage(root *Command) string {
	if root == nil {
		return ""
	}
	names := make([]string, 0, len(root.Children))
	for _, child := range root.Children {
		if child == nil || strings.TrimSpace(child.Use) == "" {
			continue
		}
		names = append(names, strings.TrimSpace(child.Use))
	}
	sort.Strings(names)
	return fmt.Sprintf("available commands: %s", strings.Join(names, ", "))
}
