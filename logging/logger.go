// Package logging 提供统一的日志接口抽象。
package logging

import (
	"context"
	"encoding/json"
	"fmt"
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

// StdLogger 基于标准库 `log` 提供最简单的文本日志实现。
type StdLogger struct {
	prefix string
	fields []Field
}

// NewStdLogger 创建一个使用固定前缀的标准库日志实现。
func NewStdLogger(prefix string) *StdLogger {
	return &StdLogger{prefix: prefix, fields: make([]Field, 0)}
}

// Logger 定义支持级别过滤与文本/JSON 输出的 logger。
type Logger struct {
	prefix string
	level  Level
	format Format
	fields []Field
}

// NewLogger 创建支持运行时配置的 logger。
func NewLogger(cfg Config) *Logger {
	format := cfg.Format
	if format == "" {
		format = TextFormat
	}
	return &Logger{
		prefix: cfg.Prefix,
		level:  cfg.Level,
		format: format,
		fields: make([]Field, 0),
	}
}

type normalizedFields struct {
	component string
	event     string
	others    []Field
}

// format 把消息和结构化字段拼成一行文本日志。
func (l *StdLogger) format(msg string, fields ...Field) string {
	return formatTextEntry(l.prefix, msg, normalizeFields(l.fields, fields...))
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

func formatTextEntry(prefix string, msg string, normalized normalizedFields) string {
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(prefix)
	}
	if normalized.component != "" {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('[')
		sb.WriteString(normalized.component)
		sb.WriteByte(']')
	}
	if normalized.event != "" {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString("event=")
		sb.WriteString(normalized.event)
	}
	if msg != "" {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(msg)
	}
	for _, f := range normalized.others {
		sb.WriteByte(' ')
		sb.WriteString(f.Key)
		sb.WriteByte('=')
		sb.WriteString(formatValue(f.Value))
	}
	return sb.String()
}

func jsonValue(v any) any {
	if v == nil || isTypedNil(v) {
		return nil
	}
	switch val := v.(type) {
	case error:
		return val.Error()
	case fmt.Stringer:
		return val.String()
	case time.Duration:
		return val.String()
	case time.Time:
		return val.Format(time.RFC3339Nano)
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return val
	default:
		if _, err := json.Marshal(val); err == nil {
			return val
		}
		return formatValue(val)
	}
}

func logJSONLine(level Level, prefix string, msg string, normalized normalizedFields) {
	payload := map[string]any{
		"time":  time.Now().Format(time.RFC3339Nano),
		"level": level.String(),
	}
	if prefix != "" {
		payload["logger"] = prefix
	}
	if normalized.component != "" {
		payload["component"] = normalized.component
	}
	if normalized.event != "" {
		payload["event"] = normalized.event
	}
	if msg != "" {
		payload["message"] = msg
	}
	for _, f := range normalized.others {
		payload[f.Key] = jsonValue(f.Value)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Println("[ERROR]", formatTextEntry(prefix, "marshal json log failed", normalizedFields{
			others: []Field{Error(err), String("fallback_message", msg)},
		}))
		return
	}
	_, _ = fmt.Fprintln(log.Writer(), string(encoded))
}

func (l *Logger) log(level Level, ctx context.Context, msg string, fields ...Field) {
	if level < l.level {
		return
	}
	fields = mergeContextFields(ctx, fields)
	normalized := normalizeFields(l.fields, fields...)
	if l.format == JSONFormat {
		logJSONLine(level, l.prefix, msg, normalized)
		return
	}
	label := strings.ToUpper(level.String())
	log.Println("["+label+"]", formatTextEntry(l.prefix, msg, normalized))
}

// Debug 记录一条调试级文本日志。
func (l *StdLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	log.Println("[DEBUG]", l.format(msg, fields...))
}

// Info 记录一条信息级文本日志。
func (l *StdLogger) Info(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	log.Println("[INFO]", l.format(msg, fields...))
}

// Warn 记录一条告警级文本日志。
func (l *StdLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	log.Println("[WARN]", l.format(msg, fields...))
}

// Error 记录一条错误级文本日志。
func (l *StdLogger) Error(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	log.Println("[ERROR]", l.format(msg, fields...))
}

func (l *StdLogger) WithFields(fields ...Field) ILogger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	return &StdLogger{prefix: l.prefix, fields: newFields}
}

func (l *StdLogger) WithField(key string, value any) ILogger {
	return l.WithFields(Field{Key: key, Value: value})
}

// Debug 按配置级别输出调试日志。
func (l *Logger) Debug(ctx context.Context, msg string, fields ...Field) {
	l.log(DebugLevel, ctx, msg, fields...)
}

// Info 按配置级别输出信息日志。
func (l *Logger) Info(ctx context.Context, msg string, fields ...Field) {
	l.log(InfoLevel, ctx, msg, fields...)
}

// Warn 按配置级别输出告警日志。
func (l *Logger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.log(WarnLevel, ctx, msg, fields...)
}

// Error 按配置级别输出错误日志。
func (l *Logger) Error(ctx context.Context, msg string, fields ...Field) {
	l.log(ErrorLevel, ctx, msg, fields...)
}

func (l *Logger) WithFields(fields ...Field) ILogger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	return &Logger{prefix: l.prefix, level: l.level, format: l.format, fields: newFields}
}

func (l *Logger) WithField(key string, value any) ILogger {
	return l.WithFields(Field{Key: key, Value: value})
}

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

var (
	defaultFactoryMu sync.RWMutex
	defaultFactory   func() ILogger
)

// SetDefaultFactory 设置 nil logger 的默认工厂。
func SetDefaultFactory(factory func() ILogger) {
	defaultFactoryMu.Lock()
	defer defaultFactoryMu.Unlock()
	defaultFactory = factory
}

// ResetDefaultFactory 清理默认 logger 工厂。
func ResetDefaultFactory() {
	SetDefaultFactory(nil)
}

func newDefaultLogger() ILogger {
	defaultFactoryMu.RLock()
	factory := defaultFactory
	defaultFactoryMu.RUnlock()
	if factory == nil {
		return nil
	}
	return factory()
}

// ensureLogger 返回一个非 nil 的 logger，避免 nil logger 在调用端引发 panic。
func ensureLogger(logger ILogger) ILogger {
	if logger == nil {
		if configured := newDefaultLogger(); configured != nil {
			return configured
		}
		return NewStdLogger("")
	}
	return logger
}

// WithComponent 为 logger 注入 component 字段，返回新的 logger。
func WithComponent(logger ILogger, component string) ILogger {
	logger = ensureLogger(logger)
	if component == "" {
		return logger
	}
	return logger.WithField("component", component)
}

// ComponentLogger 为 logger 注入组件名字段，便于统一标记日志来源。
func ComponentLogger(component string, base ...ILogger) ILogger {
	var logger ILogger
	if len(base) > 0 {
		logger = base[0]
	}
	return WithComponent(logger, component)
}
