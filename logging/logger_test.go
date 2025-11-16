package logging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
)

// TestFieldConstructors 测试字段构造函数
func TestFieldConstructors(t *testing.T) {
	tests := []struct {
		name     string
		field    Field
		wantKey  string
		wantType string
	}{
		{
			name:     "String字段",
			field:    String("name", "test"),
			wantKey:  "name",
			wantType: "string",
		},
		{
			name:     "Int字段",
			field:    Int("count", 123),
			wantKey:  "count",
			wantType: "int",
		},
		{
			name:     "Int64字段",
			field:    Int64("id", int64(456)),
			wantKey:  "id",
			wantType: "int64",
		},
		{
			name:     "Uint64字段",
			field:    Uint64("timestamp", uint64(789)),
			wantKey:  "timestamp",
			wantType: "uint64",
		},
		{
			name:     "Float64字段",
			field:    Float64("price", 12.34),
			wantKey:  "price",
			wantType: "float64",
		},
		{
			name:     "Bool字段",
			field:    Bool("active", true),
			wantKey:  "active",
			wantType: "bool",
		},
		{
			name:     "Any字段",
			field:    Any("data", map[string]int{"a": 1}),
			wantKey:  "data",
			wantType: "any",
		},
		{
			name:     "Error字段",
			field:    Error(errors.New("test error")),
			wantKey:  "error",
			wantType: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field.Key != tt.wantKey {
				t.Errorf("Key = %s, 期望 %s", tt.field.Key, tt.wantKey)
			}
			if tt.field.Value == nil {
				t.Error("Value为nil")
			}
		})
	}
}

// TestFormatValue 测试值格式化
func TestFormatValue(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{
			name:  "字符串",
			value: "test",
			want:  "test",
		},
		{
			name:  "错误",
			value: errors.New("error message"),
			want:  "error message",
		},
		{
			name:  "整数",
			value: 123,
			want:  "123",
		},
		{
			name:  "布尔值",
			value: true,
			want:  "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.value)
			if got != tt.want {
				t.Errorf("formatValue() = %s, 期望 %s", got, tt.want)
			}
		})
	}
}

// TestNewStdLogger 测试标准Logger创建
func TestNewStdLogger(t *testing.T) {
	logger := NewStdLogger("test-prefix")

	if logger == nil {
		t.Fatal("Logger创建失败")
	}
	if logger.prefix != "test-prefix" {
		t.Errorf("prefix = %s, 期望 test-prefix", logger.prefix)
	}
	if logger.fields == nil {
		t.Error("fields未初始化")
	}
}

// TestStdLogger_Debug 测试Debug日志
func TestStdLogger_Debug(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Debug(ctx, "debug message", String("key", "value"))

	output := buf.String()
	if !strings.Contains(output, "[DEBUG]") {
		t.Error("输出不包含[DEBUG]")
	}
	if !strings.Contains(output, "debug message") {
		t.Error("输出不包含消息")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("输出不包含字段")
	}
}

// TestStdLogger_Info 测试Info日志
func TestStdLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Info(ctx, "info message", Int("count", 123))

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Error("输出不包含[INFO]")
	}
	if !strings.Contains(output, "info message") {
		t.Error("输出不包含消息")
	}
	if !strings.Contains(output, "count=123") {
		t.Error("输出不包含字段")
	}
}

// TestStdLogger_Warn 测试Warn日志
func TestStdLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Warn(ctx, "warn message", Bool("critical", true))

	output := buf.String()
	if !strings.Contains(output, "[WARN]") {
		t.Error("输出不包含[WARN]")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("输出不包含消息")
	}
	if !strings.Contains(output, "critical=true") {
		t.Error("输出不包含字段")
	}
}

// TestStdLogger_Error 测试Error日志
func TestStdLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Error(ctx, "error message", Error(errors.New("test error")))

	output := buf.String()
	if !strings.Contains(output, "[ERROR]") {
		t.Error("输出不包含[ERROR]")
	}
	if !strings.Contains(output, "error message") {
		t.Error("输出不包含消息")
	}
	if !strings.Contains(output, "error=test error") {
		t.Error("输出不包含错误字段")
	}
}

// TestStdLogger_WithFields 测试WithFields
func TestStdLogger_WithFields(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	loggerWithFields := logger.WithFields(
		String("module", "auth"),
		String("user", "admin"),
	)

	ctx := context.Background()
	loggerWithFields.Info(ctx, "login", String("ip", "192.168.1.1"))

	output := buf.String()
	if !strings.Contains(output, "module=auth") {
		t.Error("输出不包含module字段")
	}
	if !strings.Contains(output, "user=admin") {
		t.Error("输出不包含user字段")
	}
	if !strings.Contains(output, "ip=192.168.1.1") {
		t.Error("输出不包含ip字段")
	}
}

// TestStdLogger_WithFields_Immutable 测试WithFields不改变原Logger
func TestStdLogger_WithFields_Immutable(t *testing.T) {
	logger := NewStdLogger("test")
	originalFieldsCount := len(logger.fields)

	loggerWithFields := logger.WithFields(String("key", "value"))

	// 原Logger的fields应该不变
	if len(logger.fields) != originalFieldsCount {
		t.Error("WithFields改变了原Logger的fields")
	}

	// 新Logger应该有额外的字段
	newLogger := loggerWithFields.(*StdLogger)
	if len(newLogger.fields) != originalFieldsCount+1 {
		t.Errorf("新Logger的fields数量 = %d, 期望 %d", len(newLogger.fields), originalFieldsCount+1)
	}
}

// TestNoopLogger 测试NoopLogger
func TestNoopLogger(t *testing.T) {
	logger := NewNoopLogger()
	ctx := context.Background()

	// 所有方法都应该不panic
	logger.Debug(ctx, "test")
	logger.Info(ctx, "test")
	logger.Warn(ctx, "test")
	logger.Error(ctx, "test")

	// WithFields应该返回自身
	newLogger := logger.WithFields(String("key", "value"))
	if newLogger != logger {
		t.Error("NoopLogger.WithFields应该返回自身")
	}
}

// TestGlobalLogger 测试全局Logger
func TestGlobalLogger(t *testing.T) {
	// 保存原全局Logger
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	// 设置新的Logger
	testLogger := NewNoopLogger()
	SetLogger(testLogger)

	// 验证全局Logger已更新
	if GetLogger() != testLogger {
		t.Error("全局Logger未正确设置")
	}
}

// TestStdLogger_MultipleFields 测试多个字段
func TestStdLogger_MultipleFields(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Info(ctx, "complex log",
		String("str", "value"),
		Int("int", 123),
		Int64("int64", int64(456)),
		Bool("bool", true),
		Float64("float", 12.34),
	)

	output := buf.String()
	expectedFields := []string{
		"str=value",
		"int=123",
		"int64=456",
		"bool=true",
		"float=12.34",
	}

	for _, expected := range expectedFields {
		if !strings.Contains(output, expected) {
			t.Errorf("输出不包含字段: %s", expected)
		}
	}
}

// TestStdLogger_EmptyPrefix 测试空前缀
func TestStdLogger_EmptyPrefix(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("")
	ctx := context.Background()

	logger.Info(ctx, "message")

	output := buf.String()
	if !strings.Contains(output, "message") {
		t.Error("输出不包含消息")
	}
}

// TestStdLogger_NoFields 测试无字段日志
func TestStdLogger_NoFields(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Info(ctx, "simple message")

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Error("输出不包含[INFO]")
	}
	if !strings.Contains(output, "simple message") {
		t.Error("输出不包含消息")
	}
}

// TestLoggerInterface 测试Logger接口实现
func TestLoggerInterface(t *testing.T) {
	// 验证StdLogger实现了Logger接口
	var _ Logger = (*StdLogger)(nil)
	var _ Logger = (*NoopLogger)(nil)

	// 捕获标准输出，避免日志输出引发panic
	oldWriter := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldWriter)

	// 创建实例测试
	stdLogger := NewStdLogger("test")
	noopLogger := NewNoopLogger()

	loggers := []Logger{stdLogger, noopLogger}
	ctx := context.Background()

	for _, logger := range loggers {
		// 所有方法都应该可以调用
		logger.Debug(ctx, "test")
		logger.Info(ctx, "test")
		logger.Warn(ctx, "test")
		logger.Error(ctx, "test")
		logger.WithFields(String("key", "value"))
	}
}

// BenchmarkStdLogger_Info 基准测试：Info日志
func BenchmarkStdLogger_Info(b *testing.B) {
	logger := NewStdLogger("bench")
	ctx := context.Background()
	log.SetOutput(&bytes.Buffer{}) // 丢弃输出
	defer log.SetOutput(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message", String("key", "value"))
	}
}

// BenchmarkStdLogger_WithFields 基准测试：WithFields
func BenchmarkStdLogger_WithFields(b *testing.B) {
	logger := NewStdLogger("bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithFields(
			String("key1", "value1"),
			String("key2", "value2"),
			Int("count", 123),
		)
	}
}

// BenchmarkNoopLogger_Info 基准测试：NoopLogger
func BenchmarkNoopLogger_Info(b *testing.B) {
	logger := NewNoopLogger()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message", String("key", "value"))
	}
}

// BenchmarkFieldConstructors 基准测试：字段构造
func BenchmarkFieldConstructors(b *testing.B) {
	b.Run("String", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			String("key", "value")
		}
	})

	b.Run("Int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Int("count", 123)
		}
	})

	b.Run("Error", func(b *testing.B) {
		err := errors.New("test error")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Error(err)
		}
	})
}
