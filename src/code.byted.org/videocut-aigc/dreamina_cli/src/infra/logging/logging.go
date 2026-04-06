package logging

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

type submitIDKey struct{}

// WithSubmitID 把 submit_id 写入上下文，供日志输出时自动带上任务标识。
func WithSubmitID(ctx context.Context, submitID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	submitID = strings.TrimSpace(submitID)
	if submitID == "" {
		return ctx
	}
	return context.WithValue(ctx, submitIDKey{}, submitID)
}

// InfofContext 输出带上下文的 INFO 日志；仅在调试开关开启时实际生效。
func InfofContext(ctx context.Context, format string, args ...any) {
	if !debugEnabled() {
		return
	}
	writeLogLine("INFO", ctx, format, args...)
}

// ErrorfContext 输出带上下文的 ERROR 日志。
func ErrorfContext(ctx context.Context, format string, args ...any) {
	writeLogLine("ERROR", ctx, format, args...)
}

// writeLogLine 统一拼接时间、级别和 submit_id 前缀后写到 stderr。
func writeLogLine(level string, ctx context.Context, format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	if message == "" {
		return
	}
	prefix := []string{time.Now().Format(time.RFC3339), level}
	if submitID, ok := ctx.Value(submitIDKey{}).(string); ok && strings.TrimSpace(submitID) != "" {
		prefix = append(prefix, "submit_id="+strings.TrimSpace(submitID))
	}
	_, _ = fmt.Fprintf(os.Stderr, "[%s] %s\n", strings.Join(prefix, " "), message)
}

// debugEnabled 根据调试环境变量判断是否输出调试级日志。
func debugEnabled() bool {
	for _, key := range []string{"DREAMINA_DEBUG", "DREAMINA_TRACE"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if value == "1" || value == "true" || value == "yes" {
			return true
		}
	}
	return false
}
