package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"code.byted.org/videocut-aigc/dreamina_cli/components/login"
)

// newImportLoginResponseCommand 创建导入完整登录响应 JSON 的命令入口。
func newImportLoginResponseCommand(app any) *Command {
	// import_login_response 支持从 --file 或 stdin 读取完整登录响应 JSON，
	// 然后直接走本地凭证导入流程。
	return &Command{
		Use: "import_login_response",
		RunE: func(cmd *Command, args []string) error {
			file, err := importLoginResponseFileArg(args)
			if err != nil {
				return err
			}

			body, err := readImportLoginResponseBody(cmd, file)
			if err != nil {
				return err
			}

			svc, err := login.NewService()
			if err != nil {
				return err
			}

			return svc.ImportLoginResponse(cmd.Context(), body, cmd.OutOrStdout())
		},
	}
}

// readImportLoginResponseBody 从 --file 或 stdin 读取完整登录响应 JSON 正文。
func readImportLoginResponseBody(cmd any, file string) ([]byte, error) {
	// 优先读取 --file，对未管道输入的交互式 stdin 则直接拒绝，避免用户空跑命令。
	command, ok := cmd.(*Command)
	if !ok {
		return nil, fmt.Errorf("invalid command context")
	}

	file = strings.TrimSpace(file)
	if file != "" {
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			return nil, fmt.Errorf("login response file is empty")
		}
		return body, nil
	}

	reader := command.InOrStdin()
	if fileReader, ok := reader.(*os.File); ok {
		info, err := fileReader.Stat()
		if err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
			return nil, fmt.Errorf("please pass --file or pipe the full JSON body into dreamina import_login_response")
		}
	}

	body, err := io.ReadAll(io.LimitReader(reader, 0xA00000))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("stdin is empty; please pass --file or pipe the full JSON body into dreamina import_login_response")
	}
	return body, nil
}

// importLoginResponseFileArg 从命令参数里提取 --file 对应的文件路径。
func importLoginResponseFileArg(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return "", fmt.Errorf("unknown command %q for %q", arg, "dreamina import_login_response")
		}
		if arg == "--file" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", fmt.Errorf("flag needs an argument: --file")
			}
			return args[i+1], nil
		}
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(key, "="); idx >= 0 {
				key = key[:idx]
			}
			if key != "file" {
				return "", fmt.Errorf("unknown flag: --%s", key)
			}
		}
		if strings.HasPrefix(arg, "--file=") {
			return strings.TrimPrefix(arg, "--file="), nil
		}
	}
	return "", nil
}
