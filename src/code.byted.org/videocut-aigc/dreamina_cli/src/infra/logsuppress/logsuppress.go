package logsuppress

import "fmt"

// RunQuietly 执行一个安静模式任务包装；当前只负责调用传入闭包或直接返回已有错误。
func RunQuietly(v ...any) error {
	for _, arg := range v {
		switch value := arg.(type) {
		case func() error:
			return value()
		case error:
			return value
		}
	}
	return fmt.Errorf("quiet task is not provided")
}
