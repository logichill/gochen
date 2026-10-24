// Package logging 提供统一的日志接口抽象。
package logging

import (
	"context"
)

// ILogger 定义框架内部使用的最小日志接口。
type ILogger interface {
	// Debug 记录调试级日志。
	Debug(ctx context.Context, msg string, fields ...Field)

	// Info 记录信息级日志。
	Info(ctx context.Context, msg string, fields ...Field)

	// Warn 记录告警级日志。
	Warn(ctx context.Context, msg string, fields ...Field)

	// Error 记录错误级日志。
	Error(ctx context.Context, msg string, fields ...Field)

	// WithFields 追加一组结构化字段并返回新的 logger 视图。
	WithFields(fields ...Field) ILogger

	// WithField 追加单个结构化字段，是 WithFields 的语法糖。
	WithField(key string, value any) ILogger
}

// ensureLogger 返回一个非 nil 的 logger，避免 nil logger 在调用端引发 panic。
func ensureLogger(logger ILogger) ILogger {
	if logger == nil {
		return NewStdLogger("")
	}
	return logger
}
