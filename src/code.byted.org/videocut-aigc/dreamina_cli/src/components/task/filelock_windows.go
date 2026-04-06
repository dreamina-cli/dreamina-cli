//go:build windows

package task

import (
	"fmt"
	"os"
)

// Windows 版本先保留最小实现：确保锁文件存在，并在使用期间持有句柄。
// 这样至少可以保证跨平台构建和基础运行路径可用，后续如果需要更强的
// 跨进程互斥，可以再替换为 LockFileEx 实现。

type fileLock struct {
	file *os.File
}

func lockFile(v ...any) (*fileLock, error) {
	path := ""
	for _, arg := range v {
		if text, ok := arg.(string); ok && text != "" {
			path = text
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("lock path is required")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &fileLock{file: file}, nil
}

func (l *fileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}
