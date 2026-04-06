package envsetup

import (
	"os"

	"code.byted.org/videocut-aigc/dreamina_cli/config"
)

// Apply 预创建 CLI 运行所需目录，并在缺失时补默认配置目录环境变量。
func Apply(v ...any) error {
	// 当前用途：
	// - normalize environment expected by the CLI runtime
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, path := range []string{cfg.Dir, cfg.LogsDirValue} {
		if path == "" {
			continue
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	if os.Getenv("DREAMINA_CONFIG_DIR") == "" && cfg.Dir != "" {
		_ = os.Setenv("DREAMINA_CONFIG_DIR", cfg.Dir)
	}
	return nil
}
