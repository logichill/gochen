// Package logging 提供统一的日志接口抽象
package logging

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Level 日志级别
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Logger 日志接口
type Logger interface {
	// Debug 调试日志
	Debug(ctx context.Context, msg string, fields ...Field)

	// Info 信息日志
	Info(ctx context.Context, msg string, fields ...Field)

	// Warn 警告日志
	Warn(ctx context.Context, msg string, fields ...Field)

	// Error 错误日志
	Error(ctx context.Context, msg string, fields ...Field)

	// WithFields 添加字段，返回新的Logger
	WithFields(fields ...Field) Logger

	// 带错误的日志方法（用于兼容旧代码）
	DebugWithError(ctx context.Context, err error, msg string, fields ...Field)
	InfoWithError(ctx context.Context, err error, msg string, fields ...Field)
	WarnWithError(ctx context.Context, err error, msg string, fields ...Field)
	ErrorWithError(ctx context.Context, err error, msg string, fields ...Field)

	// 格式化日志方法（用于兼容旧代码）
	Debugf(ctx context.Context, format string, args ...any)
	Infof(ctx context.Context, format string, args ...any)
	Warnf(ctx context.Context, format string, args ...any)
	Errorf(ctx context.Context, format string, args ...any)
}

// Field 日志字段
type Field struct {
	Key   string
	Value any
}

// 字段构造函数
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

func Uint64(key string, value uint64) Field {
	return Field{Key: key, Value: value}
}

func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func Error(err error) Field {
	return Field{Key: "error", Value: err}
}

// Duration 以 time.Duration 作为字段值，格式化输出
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

// StdLogger 标准库log实现
type StdLogger struct {
	prefix string
	fields []Field
}

// NewStdLogger 创建标准库Logger
func NewStdLogger(prefix string) *StdLogger {
	return &StdLogger{
		prefix: prefix,
		fields: make([]Field, 0),
	}
}

func (l *StdLogger) format(msg string, fields ...Field) string {
	result := l.prefix + " " + msg
	allFields := append(l.fields, fields...)
	for _, f := range allFields {
		result += " " + f.Key + "=" + formatValue(f.Value)
	}
	return result
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case error:
		return val.Error()
	default:
		return fmt.Sprint(val)
	}
}

func (l *StdLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	log.Println("[DEBUG]", l.format(msg, fields...))
}

func (l *StdLogger) Info(ctx context.Context, msg string, fields ...Field) {
	log.Println("[INFO]", l.format(msg, fields...))
}

func (l *StdLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	log.Println("[WARN]", l.format(msg, fields...))
}

func (l *StdLogger) Error(ctx context.Context, msg string, fields ...Field) {
	log.Println("[ERROR]", l.format(msg, fields...))
}

func (l *StdLogger) WithFields(fields ...Field) Logger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	return &StdLogger{
		prefix: l.prefix,
		fields: newFields,
	}
}

// 带错误的日志方法实现
func (l *StdLogger) DebugWithError(ctx context.Context, err error, msg string, fields ...Field) {
	allFields := append(fields, Error(err))
	l.Debug(ctx, msg, allFields...)
}

func (l *StdLogger) InfoWithError(ctx context.Context, err error, msg string, fields ...Field) {
	allFields := append(fields, Error(err))
	l.Info(ctx, msg, allFields...)
}

func (l *StdLogger) WarnWithError(ctx context.Context, err error, msg string, fields ...Field) {
	allFields := append(fields, Error(err))
	l.Warn(ctx, msg, allFields...)
}

func (l *StdLogger) ErrorWithError(ctx context.Context, err error, msg string, fields ...Field) {
	allFields := append(fields, Error(err))
	l.Error(ctx, msg, allFields...)
}

// 格式化日志方法实现
func (l *StdLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.Debug(ctx, fmt.Sprintf(format, args...))
}

func (l *StdLogger) Infof(ctx context.Context, format string, args ...any) {
	l.Info(ctx, fmt.Sprintf(format, args...))
}

func (l *StdLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.Warn(ctx, fmt.Sprintf(format, args...))
}

func (l *StdLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.Error(ctx, fmt.Sprintf(format, args...))
}

// NoopLogger 空日志实现（用于测试）
type NoopLogger struct{}

func NewNoopLogger() *NoopLogger {
	return &NoopLogger{}
}

func (l *NoopLogger) Debug(ctx context.Context, msg string, fields ...Field) {}
func (l *NoopLogger) Info(ctx context.Context, msg string, fields ...Field)  {}
func (l *NoopLogger) Warn(ctx context.Context, msg string, fields ...Field)  {}
func (l *NoopLogger) Error(ctx context.Context, msg string, fields ...Field) {}
func (l *NoopLogger) WithFields(fields ...Field) Logger                      { return l }

// 带错误的日志方法（空实现）
func (l *NoopLogger) DebugWithError(ctx context.Context, err error, msg string, fields ...Field) {}
func (l *NoopLogger) InfoWithError(ctx context.Context, err error, msg string, fields ...Field)  {}
func (l *NoopLogger) WarnWithError(ctx context.Context, err error, msg string, fields ...Field)  {}
func (l *NoopLogger) ErrorWithError(ctx context.Context, err error, msg string, fields ...Field) {}

// 格式化日志方法（空实现）
func (l *NoopLogger) Debugf(ctx context.Context, format string, args ...any) {}
func (l *NoopLogger) Infof(ctx context.Context, format string, args ...any)  {}
func (l *NoopLogger) Warnf(ctx context.Context, format string, args ...any)  {}
func (l *NoopLogger) Errorf(ctx context.Context, format string, args ...any) {}

// 全局Logger
var globalLogger Logger = NewStdLogger("")

// SetLogger 设置全局Logger
func SetLogger(logger Logger) {
	globalLogger = logger
}

// GetLogger 获取全局Logger
func GetLogger() Logger {
	return globalLogger
}
