package cmd

import (
	"encoding/json"

	"code.byted.org/videocut-aigc/dreamina_cli/buildinfo"
)

// newVersionCommand 创建输出构建版本信息的命令入口。
func newVersionCommand() *Command {
	// 当前命令：
	// - version
	return &Command{
		Use: "version",
		RunE: func(cmd *Command, args []string) error {
			if err := rejectUnexpectedCommandArgs("version", args); err != nil {
				return err
			}
			return runVersion(cmd.OutOrStdout())
		},
	}
}

// runVersion 把当前构建版本信息编码成 JSON 并写到给定输出流。
func runVersion(v ...any) error {
	// 当前输出：
	// {
	//   "version": "4946b9d-dirty",
	//   "commit": "4946b9d",
	//   "build_time": "2026-03-31T07:24:44Z"
	// }
	type versionInfo struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuildTime string `json:"build_time"`
	}

	out := versionInfo{
		Version:   buildinfo.Version,
		Commit:    buildinfo.Commit,
		BuildTime: buildinfo.BuildTime,
	}

	for _, arg := range v {
		writer, ok := arg.(interface{ Write([]byte) (int, error) })
		if !ok {
			continue
		}
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	return nil
}
