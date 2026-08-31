package httpx

import "testing"

func TestSanitizeIdentifierFromHeader(t *testing.T) {
	if got := SanitizeIdentifierFromHeader("  abc-DEF_01.:  ", 128); got != "abc-DEF_01.:" {
		t.Fatalf("unexpected sanitize result: %q", got)
	}
	if got := SanitizeIdentifierFromHeader("has space", 128); got != "" {
		t.Fatalf("expected empty for invalid chars, got %q", got)
	}
	if got := SanitizeIdentifierFromHeader("中文", 128); got != "" {
		t.Fatalf("expected empty for non-ascii, got %q", got)
	}
	if got := SanitizeIdentifierFromHeader("0123456789", 5); got != "" {
		t.Fatalf("expected empty for over maxLen, got %q", got)
	}
}
