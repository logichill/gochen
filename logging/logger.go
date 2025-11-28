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
	// 统一布局（类 log4j）：
	// <prefix/service> component=... event=... msg... key=value...
	allFields := append(append([]Field{}, l.fields...), fields...)

	var component, event string
	otherFields := make([]Field, 0, len(allFields))

	for _, f := range allFields {
		switch f.Key {
		case "component":
			component = formatValue(f.Value)
		case "event":
			event = formatValue(f.Value)
		default:
			otherFields = append(otherFields, f)
		}
	}

	result := ""

	// 将 prefix 视为“服务名”或 logger 名
	if l.prefix != "" {
		result += l.prefix
	}

	// 核心维度字段优先输出，便于扫描和过滤
	if component != "" {
		if result != "" {
			result += " "
		}
		result += "[" + component + "]"
	}
	if event != "" {
		if result != "" {
			result += " "
		}
		result += "event=" + event
	}

	// 主消息放在核心字段之后
	if msg != "" {
		if result != "" {
			result += " "
		}
		result += msg
	}

	// 其余字段按 key=value 追加
	for _, f := range otherFields {
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
