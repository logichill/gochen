package errors

import "testing"

func TestPublicMessage(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{InvalidInput, "invalid input"},
		{PayloadTooLarge, "payload too large"},
		{NotFound, "not found"},
		{Conflict, "conflict"},
		{FailedPrecondition, "failed precondition"},
		{Unauthorized, "unauthorized"},
		{Forbidden, "forbidden"},
		{Timeout, "timeout"},
		{TooManyRequests, "too many requests"},
		{ServiceUnavailable, "service unavailable"},
		{Unsupported, "unsupported operation"},
		{Validation, "validation failed"},
		{Duplicate, "duplicate"},
		{Internal, ""},
		{ErrorCode("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := PublicMessage(tt.code); got != tt.want {
				t.Fatalf("PublicMessage(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
