package errors

import "testing"

func TestErrorCodeToHTTPStatus_PreconditionAndConflictBoundaries(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want int
	}{
		{FailedPrecondition, 412},
		{Conflict, 409},
		{Duplicate, 409},
		{Concurrency, 409},
	}

	for _, tt := range tests {
		if got := ErrorCodeToHTTPStatus(tt.code); got != tt.want {
			t.Fatalf("ErrorCodeToHTTPStatus(%s) = %d, want %d", tt.code, got, tt.want)
		}
		if got := ToHTTPStatus(NewCode(tt.code, "test")); got != tt.want {
			t.Fatalf("ToHTTPStatus(%s) = %d, want %d", tt.code, got, tt.want)
		}
	}
}
