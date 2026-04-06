//go:build !windows

package task

import (
	"fmt"
	"os"
	"syscall"
)

// Original tree included filelock_unix.go, which strongly suggests an advisory
// filesystem lock around the local sqlite/task store.

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
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &fileLock{file: file}, nil
}

func (l *fileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer l.file.Close()
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
