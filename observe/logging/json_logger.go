package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// Logger 定义支持级别过滤与文本/JSON 输出的 logger。
type Logger struct {
	prefix string
	level  Level
	format Format
	fields []Field
	writer io.Writer
	logger *log.Logger
	mu     *sync.Mutex
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
		writer: cfg.Writer,
		logger: newLineLogger(cfg.Writer),
		mu:     &sync.Mutex{},
	}
}

func jsonValue(v any) any {
	if v == nil || isTypedNil(v) {
		return nil
	}
	switch val := v.(type) {
	case error:
		return val.Error()
	case time.Duration:
		return val.String()
	case time.Time:
		return val.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return val.String()
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

func logJSONLine(logger *log.Logger, mu *sync.Mutex, writer io.Writer, level Level, prefix string, msg string, normalized normalizedFields) {
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
		writeLogLine(logger, "[ERROR]", formatTextEntry(prefix, "marshal json log failed", normalizedFields{
			others: []Field{Error(err), String("fallback_message", msg)},
		}))
		return
	}
	writeRawLine(mu, writer, string(encoded))
}

func (l *Logger) log(level Level, ctx context.Context, msg string, fields ...Field) {
	if level < l.level {
		return
	}
	fields = mergeContextFields(ctx, fields)
	normalized := normalizeFields(l.fields, fields...)
	if l.format == JSONFormat {
		logJSONLine(l.logger, l.mu, l.writer, level, l.prefix, msg, normalized)
		return
	}
	label := strings.ToUpper(level.String())
	writeLogLine(l.logger, "["+label+"]", formatTextEntry(l.prefix, msg, normalized))
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
	return &Logger{prefix: l.prefix, level: l.level, format: l.format, fields: newFields, writer: l.writer, logger: l.logger, mu: l.mu}
}

func (l *Logger) WithField(key string, value any) ILogger {
	return l.WithFields(Field{Key: key, Value: value})
}
