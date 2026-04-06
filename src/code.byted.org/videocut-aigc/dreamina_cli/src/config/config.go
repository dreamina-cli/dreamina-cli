package config

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultLoginCallbackPort = 60713

type Config struct {
	// 当前配置字段：
	// - state under ~/.dreamina_cli
	// - credential path: credential.json
	// - login callback server port
	// - login page / callback urls
	// - task sqlite path
	// - logs directory
	Dir             string
	CredentialPath  string
	TaskDBPathValue string
	LogsDirValue    string
	LoginURL        string
	CallbackBaseURL string
	Headers         map[string]string
}

// Load 加载 CLI 运行配置，并补齐默认目录、登录 URL、任务库和日志目录路径。
func Load() (*Config, error) {
	// 当前行为：
	// - resolve ~/.dreamina_cli
	// - ensure directory exists
	// - derive credential.json / tasks.db / logs paths
	// - sanitize configured headers
	dir := Dir()
	_ = os.MkdirAll(dir, 0o755)
	return &Config{
		Dir:             dir,
		CredentialPath:  filepath.Join(dir, "credential.json"),
		TaskDBPathValue: filepath.Join(dir, "tasks.db"),
		LogsDirValue:    filepath.Join(dir, "logs"),
		LoginURL:        "https://jimeng.jianying.com/ai-tool/login",
		CallbackBaseURL: "http://127.0.0.1",
		Headers:         map[string]string{},
	}, nil
}

// Dir 返回当前 CLI 使用的配置根目录，优先读取 DREAMINA_CONFIG_DIR。
func Dir() string {
	if dir := filepath.Clean(filepath.FromSlash(strings.TrimSpace(os.Getenv("DREAMINA_CONFIG_DIR")))); dir != "." && dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".dreamina_cli"
	}
	return filepath.Join(home, ".dreamina_cli")
}

// Path 返回配置根目录路径；当前与 Dir 保持一致。
func Path() string { return Dir() }

// TaskDBPath 返回任务库文件路径。
func TaskDBPath() string { return filepath.Join(Dir(), "tasks.db") }

// LogsDir 返回日志目录路径。
func LogsDir() string { return filepath.Join(Dir(), "logs") }

// sanitizeHeaders 预留给配置头信息净化逻辑；当前恢复版尚未真正使用。
func sanitizeHeaders(v any) any {
	// 当前保留该 helper 名称。
	return nil
}
