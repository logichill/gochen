package logging

import "context"

// NoopLogger 是一个不产生任何输出的空日志实现。
type NoopLogger struct{}

// NewNoopLogger 创建一个空操作 logger。
func NewNoopLogger() *NoopLogger { return &NoopLogger{} }

// Debug 在空 logger 中忽略调试日志。
func (l *NoopLogger) Debug(ctx context.Context, msg string, fields ...Field) {}

// Info 在空 logger 中忽略信息日志。
func (l *NoopLogger) Info(ctx context.Context, msg string, fields ...Field) {}

// Warn 在空 logger 中忽略告警日志。
func (l *NoopLogger) Warn(ctx context.Context, msg string, fields ...Field) {}

// Error 在空 logger 中忽略错误日志。
func (l *NoopLogger) Error(ctx context.Context, msg string, fields ...Field) {}

func (l *NoopLogger) WithFields(fields ...Field) ILogger { return l }

func (l *NoopLogger) WithField(key string, value any) ILogger { return l }
