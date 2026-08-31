// Package logging 提供统一的日志接口抽象。
package logging

import (
	"context"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Level 表示日志级别。
type Level int

const (
	// DebugLevel 用于调试级日志。
	DebugLevel Level = iota
	// InfoLevel 用于常规信息日志。
	InfoLevel
	// WarnLevel 用于告警级日志。
	WarnLevel
	// ErrorLevel 用于错误级日志。
	ErrorLevel
)

func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	default:
		return "info"
	}
}

// ParseLevel 解析日志等级；未知值回退到 fallback。
func ParseLevel(raw string, fallback Level) Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return InfoLevel
	case "debug":
		return DebugLevel
	case "warn", "warning":
		return WarnLevel
	case "error", "fatal":
		return ErrorLevel
	default:
		return fallback
	}
}

// Format 定义日志输出格式。
type Format string

const (
	// TextFormat 表示人类可读的文本日志格式。
	TextFormat Format = "text"
	// JSONFormat 表示结构化 JSON 日志格式。
	JSONFormat Format = "json"
)

// ParseFormat 解析日志输出格式；未知值回退到 fallback。
func ParseFormat(raw string, fallback Format) Format {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "json":
		return JSONFormat
	case "", "text":
		return TextFormat
	default:
		return fallback
	}
}

// Config 定义可配置 logger 的运行参数。
type Config struct {
	Prefix string
	Level  Level
	Format Format
	Writer io.Writer
}

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

// Field 日志字段，用于在日志中附加结构化的键值对信息。
type Field struct {
	Key   string
	Value any
}

// String 构造字符串类型的日志字段。
func String(key, value string) Field { return Field{Key: key, Value: value} }

// Int 构造 int 类型的日志字段。
func Int(key string, value int) Field { return Field{Key: key, Value: value} }

// Int64 构造 int64 类型的日志字段。
func Int64(key string, value int64) Field { return Field{Key: key, Value: value} }

// Uint64 构造 uint64 类型的日志字段。
func Uint64(key string, value uint64) Field { return Field{Key: key, Value: value} }

// Float64 构造 float64 类型的日志字段。
func Float64(key string, value float64) Field { return Field{Key: key, Value: value} }

// Bool 构造 bool 类型的日志字段。
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }

// Any 构造任意值类型的日志字段。
func Any(key string, value any) Field { return Field{Key: key, Value: value} }

// Error 把错误对象放入统一的 `error` 日志字段。
func Error(err error) Field { return Field{Key: "error", Value: err} }

// Duration 构造 time.Duration 类型的日志字段。
func Duration(key string, value time.Duration) Field { return Field{Key: key, Value: value} }

type normalizedFields struct {
	component string
	event     string
	others    []Field
}

func isTypedNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// formatValue 把字段值转换为适合单行日志输出的文本。
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return escapeNewlines(val)
	case error:
		return escapeNewlines(val.Error())
	default:
		return escapeNewlines(fmt.Sprint(val))
	}
}

// escapeNewlines 把换行转义为文本，避免单条日志拆成多行。
func escapeNewlines(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r", "\\r")
	return strings.ReplaceAll(s, "\n", "\\n")
}

func normalizeFields(base []Field, fields ...Field) normalizedFields {
	allFields := append(append([]Field{}, base...), fields...)

	hasStack := false
	for _, f := range allFields {
		if f.Key == "stack" {
			hasStack = true
			break
		}
	}
	if !hasStack {
		for _, f := range allFields {
			if f.Key != "error" || f.Value == nil || isTypedNil(f.Value) {
				continue
			}
			e, ok := f.Value.(interface{ Details() map[string]any })
			if !ok {
				continue
			}
			details := e.Details()
			if details == nil {
				continue
			}
			stack, ok := details["stack"].(string)
			if !ok || strings.TrimSpace(stack) == "" {
				continue
			}
			allFields = append(allFields, Field{Key: "stack", Value: stack})
			break
		}
	}

	result := normalizedFields{others: make([]Field, 0, len(allFields))}
	for _, f := range allFields {
		switch f.Key {
		case "component":
			result.component = formatValue(f.Value)
		case "event":
			result.event = formatValue(f.Value)
		default:
			result.others = append(result.others, f)
		}
	}
	return result
}

func newLineLogger(writer io.Writer) *log.Logger {
	if writer == nil {
		return nil
	}
	return log.New(writer, "", log.LstdFlags)
}

func writeLogLine(logger *log.Logger, args ...any) {
	if logger == nil {
		log.Println(args...)
		return
	}
	logger.Println(args...)
}

func writeRawLine(mu *sync.Mutex, writer io.Writer, line string) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	if writer == nil {
		_, _ = fmt.Fprintln(log.Writer(), line)
		return
	}
	_, _ = fmt.Fprintln(writer, line)
}

// ensureLogger 返回一个非 nil 的 logger，避免 nil logger 在调用端引发 panic。
func ensureLogger(logger ILogger) ILogger {
	if logger == nil {
		return NewStdLogger("")
	}
	return logger
}
