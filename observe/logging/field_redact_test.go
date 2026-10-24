package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestIsSensitiveFieldKey(t *testing.T) {
	sensitive := []string{
		"password", "Password", "authorization", "Authorization",
		"access_token", "refresh_token", "api_key", "apiKey",
		"x-api-key", "req_header_authorization", "user_password",
		"client_secret", "cookie", "set-cookie", "session_id",
	}
	for _, key := range sensitive {
		if !isSensitiveFieldKey(key) {
			t.Fatalf("expected %q to be sensitive", key)
		}
	}
	plain := []string{"user_id", "tenant_id", "method", "path", "status", "duration_ms", "author"}
	for _, key := range plain {
		if isSensitiveFieldKey(key) {
			t.Fatalf("expected %q to be plain", key)
		}
	}
}

func TestNormalizeFieldsRedactsSensitiveValues(t *testing.T) {
	normalized := normalizeFields(nil,
		String("password", "hunter2"),
		String("access_token", "tok_123"),
		String("user_id", "u1"),
	)
	for _, f := range normalized.others {
		switch f.Key {
		case "password", "access_token":
			if f.Value != redactedValue {
				t.Fatalf("expected %s redacted, got %v", f.Key, f.Value)
			}
		case "user_id":
			if f.Value != "u1" {
				t.Fatalf("expected user_id preserved, got %v", f.Value)
			}
		}
	}
}

func TestJSONLoggerRedactsSensitiveFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(Config{Format: JSONFormat, Level: InfoLevel, Writer: &buf})
	logger.Info(context.Background(), "login",
		String("user_id", "u1"),
		String("password", "hunter2"),
		String("authorization", "Bearer tok"),
	)
	line := buf.String()
	if strings.Contains(line, "hunter2") || strings.Contains(line, "Bearer tok") {
		t.Fatalf("expected secrets redacted in output: %s", line)
	}
	if !strings.Contains(line, redactedValue) {
		t.Fatalf("expected redaction marker in output: %s", line)
	}
	if !strings.Contains(line, `"user_id":"u1"`) {
		t.Fatalf("expected plain fields preserved: %s", line)
	}
}

func TestStdLoggerRedactsSensitiveFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLoggerWithWriter("", &buf)
	logger.Info(context.Background(), "login",
		String("password", "hunter2"),
		String("user_id", "u1"),
	)
	line := buf.String()
	if strings.Contains(line, "hunter2") {
		t.Fatalf("expected secret redacted in text output: %s", line)
	}
	if !strings.Contains(line, redactedValue) || !strings.Contains(line, "u1") {
		t.Fatalf("unexpected text output: %s", line)
	}
}
