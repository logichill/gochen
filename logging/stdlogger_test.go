package logging

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"gochen/errors"
)

// TestNewStdLogger 验证 NewStdLogger。
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

// TestStdLogger_Debug 验证 StdLogger Debug。
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

// TestStdLogger_Info 验证 StdLogger Info。
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

// TestStdLogger_Warn 验证 StdLogger Warn。
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

// TestStdLogger_Error 验证 StdLogger Error。
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

func TestStdLogger_Error_AppError5xx_IncludesStackField(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Error(ctx, "error message", Error(errors.NewCode(errors.Internal, "boom")))

	output := buf.String()
	if !strings.Contains(output, "stack=") {
		t.Fatalf("output does not include stack field: %s", output)
	}
	if !strings.Contains(output, "TestStdLogger_Error_AppError5xx_IncludesStackField") {
		t.Fatalf("output does not include test function name in stack: %s", output)
	}
	if strings.Count(output, "\n") != 1 {
		t.Fatalf("expected single-line log output (except trailing newline), got: %q", output)
	}
	if !strings.Contains(output, "\\n") {
		t.Fatalf("expected escaped newline sequence in output, got: %q", output)
	}
}

func TestStdLogger_Error_AppError4xx_DoesNotIncludeStackField(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := NewStdLogger("test")
	ctx := context.Background()

	logger.Error(ctx, "error message", Error(errors.NewCode(errors.InvalidInput, "bad request")))

	output := buf.String()
	if strings.Contains(output, "stack=") {
		t.Fatalf("output includes stack field for 4xx error: %s", output)
	}
}

func TestStdLogger_Error_TypedNilAppError_DoesNotPanic(t *testing.T) {
	var appErr *errors.AppError

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	logger := NewStdLogger("test")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected no panic for typed nil app error, got %v", r)
		}
	}()

	logger.Error(context.Background(), "typed nil error", Error(appErr))
}

// TestStdLogger_WithFields 验证 StdLogger WithFields。
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

// TestStdLogger_WithFields_Immutable 验证 StdLogger WithFields Immutable。
func TestStdLogger_WithFields_Immutable(t *testing.T) {
	logger := NewStdLogger("test")
	originalFieldsCount := len(logger.fields)

	loggerWithFields := logger.WithFields(String("key", "value"))

	if len(logger.fields) != originalFieldsCount {
		t.Error("WithFields改变了原Logger的fields")
	}

	newLogger := loggerWithFields.(*StdLogger)
	if len(newLogger.fields) != originalFieldsCount+1 {
		t.Errorf("新Logger的fields数量 = %d, 期望 %d", len(newLogger.fields), originalFieldsCount+1)
	}
}

// TestStdLogger_MultipleFields 验证 StdLogger MultipleFields。
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

// TestStdLogger_EmptyPrefix 验证 StdLogger EmptyPrefix。
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

// TestStdLogger_NoFields 验证 StdLogger NoFields。
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

// BenchmarkStdLogger_Info 用于评估 StdLogger Info 的性能。
func BenchmarkStdLogger_Info(b *testing.B) {
	logger := NewStdLogger("bench")
	ctx := context.Background()
	log.SetOutput(&bytes.Buffer{})
	defer log.SetOutput(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message", String("key", "value"))
	}
}

// BenchmarkStdLogger_WithFields 用于评估 StdLogger WithFields 的性能。
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
