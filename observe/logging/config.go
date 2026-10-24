package logging

import (
	"io"
	"strings"
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
