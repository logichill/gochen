package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"
)

func TestLogger_RespectsMinLevel(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	logger := NewLogger(Config{Prefix: "app", Level: WarnLevel, Format: TextFormat})
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

func TestComponentLogger_UsesDefaultFactory(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)
	ResetDefaultFactory()
	defer ResetDefaultFactory()

	SetDefaultFactory(func() ILogger {
		return NewLogger(Config{Prefix: "app", Level: InfoLevel, Format: JSONFormat})
	})

	ComponentLogger("service.level").Info(context.Background(), "hello", String("foo", "bar"))

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
