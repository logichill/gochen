package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogger_RespectsMinLevel(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger(Config{Prefix: "app", Level: WarnLevel, Format: TextFormat, Writer: &buf})
	logger.Info(context.Background(), "hidden")
	logger.Error(context.Background(), "visible")

	output := buf.String()
	if strings.Contains(output, "hidden") {
		t.Fatalf("expected info log to be filtered, got %q", output)
	}
	if !strings.Contains(output, "visible") {
		t.Fatalf("expected error log to be emitted, got %q", output)
	}
}

func TestComponentLogger_UsesExplicitBaseLogger(t *testing.T) {
	var buf bytes.Buffer
	base := NewLogger(Config{Prefix: "app", Level: InfoLevel, Format: JSONFormat, Writer: &buf})

	ComponentLogger("service.level", base).Info(context.Background(), "hello", String("foo", "bar"))

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &payload); err != nil {
		t.Fatalf("expected json log output, got %q (%v)", buf.String(), err)
	}
	if payload["logger"] != "app" {
		t.Fatalf("expected logger prefix app, got %#v", payload["logger"])
	}
	if payload["component"] != "service.level" {
		t.Fatalf("expected component service.level, got %#v", payload["component"])
	}
	if payload["message"] != "hello" {
		t.Fatalf("expected message hello, got %#v", payload["message"])
	}
}

func TestLogger_WriterIsPerLogger(t *testing.T) {
	var first bytes.Buffer
	var second bytes.Buffer

	logger1 := NewLogger(Config{Prefix: "one", Level: InfoLevel, Format: TextFormat, Writer: &first})
	logger2 := NewLogger(Config{Prefix: "two", Level: InfoLevel, Format: JSONFormat, Writer: &second})

	logger1.Info(context.Background(), "first-only")
	logger2.Info(context.Background(), "second-only")

	if strings.Contains(first.String(), "second-only") {
		t.Fatalf("first writer received second logger output: %q", first.String())
	}
	if strings.Contains(second.String(), "first-only") {
		t.Fatalf("second writer received first logger output: %q", second.String())
	}
	if !strings.Contains(first.String(), "first-only") {
		t.Fatalf("first writer missing own output: %q", first.String())
	}
	if !strings.Contains(second.String(), "second-only") {
		t.Fatalf("second writer missing own output: %q", second.String())
	}
}

func TestLogger_JSONWriterDoesNotAddTextPrefix(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger(Config{Prefix: "json", Level: InfoLevel, Format: JSONFormat, Writer: &buf})
	logger.Info(context.Background(), "hello")

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &payload); err != nil {
		t.Fatalf("expected raw json line, got %q (%v)", buf.String(), err)
	}
	if payload["message"] != "hello" {
		t.Fatalf("expected hello message, got %#v", payload["message"])
	}
}

func TestLogger_JSONFormatsTimeFieldAsRFC3339Nano(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger(Config{Prefix: "json", Level: InfoLevel, Format: JSONFormat, Writer: &buf})
	at := time.Date(2026, 5, 29, 10, 11, 12, 123456789, time.UTC)
	logger.Info(context.Background(), "hello", Any("at", at))

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &payload); err != nil {
		t.Fatalf("expected raw json line, got %q (%v)", buf.String(), err)
	}
	if payload["at"] != at.Format(time.RFC3339Nano) {
		t.Fatalf("expected RFC3339Nano time field, got %#v", payload["at"])
	}
}

func TestLogger_WritersAreIndependentInParallel(t *testing.T) {
	var first bytes.Buffer
	var second bytes.Buffer
	var firstMu sync.Mutex
	var secondMu sync.Mutex
	firstWriter := lockedWriter{mu: &firstMu, buf: &first}
	secondWriter := lockedWriter{mu: &secondMu, buf: &second}

	logger1 := NewLogger(Config{Prefix: "one", Level: InfoLevel, Format: TextFormat, Writer: firstWriter})
	logger2 := NewLogger(Config{Prefix: "two", Level: InfoLevel, Format: TextFormat, Writer: secondWriter})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			logger1.Info(context.Background(), "first-only")
		}()
		go func() {
			defer wg.Done()
			logger2.Info(context.Background(), "second-only")
		}()
	}
	wg.Wait()

	if strings.Contains(first.String(), "second-only") {
		t.Fatalf("first writer received second logger output: %q", first.String())
	}
	if strings.Contains(second.String(), "first-only") {
		t.Fatalf("second writer received first logger output: %q", second.String())
	}
}

func TestLogger_JSONWriterConcurrent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(Config{Prefix: "json", Level: InfoLevel, Format: JSONFormat, Writer: &buf})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Info(context.Background(), "line", Int("i", i))
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 100 {
		t.Fatalf("expected 100 json lines, got %d: %q", len(lines), buf.String())
	}
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d is not valid json: %q (%v)", i, line, err)
		}
		if payload["message"] != "line" {
			t.Fatalf("line %d missing message: %s", i, fmt.Sprint(payload))
		}
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}
