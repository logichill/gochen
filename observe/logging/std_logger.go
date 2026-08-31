package logging

import (
	"context"
	"io"
	"log"
	"strings"
)

// StdLogger 基于标准库 `log` 提供最简单的文本日志实现。
type StdLogger struct {
	prefix string
	fields []Field
	writer io.Writer
	logger *log.Logger
}

// NewStdLogger 创建一个使用固定前缀的标准库日志实现。
func NewStdLogger(prefix string) *StdLogger {
	return NewStdLoggerWithWriter(prefix, nil)
}

// NewStdLoggerWithWriter 创建使用实例级 writer 的标准库日志实现。
func NewStdLoggerWithWriter(prefix string, writer io.Writer) *StdLogger {
	return &StdLogger{prefix: prefix, fields: make([]Field, 0), writer: writer, logger: newLineLogger(writer)}
}

// format 把消息和结构化字段拼成一行文本日志。
func (l *StdLogger) format(msg string, fields ...Field) string {
	return formatTextEntry(l.prefix, msg, normalizeFields(l.fields, fields...))
}

// Debug 记录一条调试级文本日志。
func (l *StdLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	writeLogLine(l.logger, "[DEBUG]", l.format(msg, fields...))
}

// Info 记录一条信息级文本日志。
func (l *StdLogger) Info(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	writeLogLine(l.logger, "[INFO]", l.format(msg, fields...))
}

// Warn 记录一条告警级文本日志。
func (l *StdLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	writeLogLine(l.logger, "[WARN]", l.format(msg, fields...))
}

// Error 记录一条错误级文本日志。
func (l *StdLogger) Error(ctx context.Context, msg string, fields ...Field) {
	fields = mergeContextFields(ctx, fields)
	writeLogLine(l.logger, "[ERROR]", l.format(msg, fields...))
}

func (l *StdLogger) WithFields(fields ...Field) ILogger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	return &StdLogger{prefix: l.prefix, fields: newFields, writer: l.writer, logger: l.logger}
}

func (l *StdLogger) WithField(key string, value any) ILogger {
	return l.WithFields(Field{Key: key, Value: value})
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
