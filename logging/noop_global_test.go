package logging

import (
	"context"
	"io"
	"log"
	"testing"
)

// TestNoopLogger 验证 NoopLogger。
func TestNoopLogger(t *testing.T) {
	logger := NewNoopLogger()
	ctx := context.Background()

	logger.Debug(ctx, "test")
	logger.Info(ctx, "test")
	logger.Warn(ctx, "test")
	logger.Error(ctx, "test")

	newLogger := logger.WithFields(String("key", "value"))
	if newLogger != logger {
		t.Error("NoopLogger.WithFields应该返回自身")
	}
}

// TestComponentLogger_NoGlobalState 验证 ComponentLogger/WithComponent 不依赖任何全局 logger，
// 且在 base=nil 时仍然安全可用。
func TestComponentLogger_NoGlobalState(t *testing.T) {
	l1 := ComponentLogger("test.component")
	if l1 == nil {
		t.Fatalf("expected ComponentLogger() return non-nil")
	}

	// base=nil: should fall back to default StdLogger and carry component field
	std, ok := l1.(*StdLogger)
	if !ok {
		t.Fatalf("expected default logger to be *StdLogger, got %T", l1)
	}
	found := false
	for _, f := range std.fields {
		if f.Key == "component" && f.Value == "test.component" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected component field to be set")
	}

	// explicit base: should still work (and not panic)
	l2 := WithComponent(NewNoopLogger(), "test.component")
	if l2 == nil {
		t.Fatalf("expected WithComponent() return non-nil")
	}
}

// TestLoggerInterface 验证 LoggerInterface。
func TestLoggerInterface(t *testing.T) {
	var _ ILogger = (*StdLogger)(nil)
	var _ ILogger = (*NoopLogger)(nil)

	oldWriter := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldWriter)

	stdLogger := NewStdLogger("test")
	noopLogger := NewNoopLogger()

	loggers := []ILogger{stdLogger, noopLogger}
	ctx := context.Background()

	for _, logger := range loggers {
		logger.Debug(ctx, "test")
		logger.Info(ctx, "test")
		logger.Warn(ctx, "test")
		logger.Error(ctx, "test")
		logger.WithFields(String("key", "value"))
	}
}

// BenchmarkNoopLogger_Info 用于评估 NoopLogger Info 的性能。
func BenchmarkNoopLogger_Info(b *testing.B) {
	logger := NewNoopLogger()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message", String("key", "value"))
	}
}
