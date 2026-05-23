package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

type customAsAppError struct {
	appErr *AppError
}

func (e customAsAppError) Error() string {
	return "custom app error"
}

func (e customAsAppError) As(target any) bool {
	appErr, ok := target.(**AppError)
	if !ok {
		return false
	}
	*appErr = e.appErr
	return true
}

func TestNormalizeTypedNilAppError(t *testing.T) {
	var appErr *AppError
	var err error = appErr

	if got := Normalize(err); got != nil {
		t.Fatalf("Normalize(typed nil AppError) = %v, want nil", got)
	}
}

func TestCodeAndHTTPStatusTypedNilAppError(t *testing.T) {
	var appErr *AppError
	var err error = appErr

	if got := Code(err); got != "" {
		t.Fatalf("Code(typed nil AppError) = %q, want empty", got)
	}
	if got := ToHTTPStatus(err); got != 200 {
		t.Fatalf("ToHTTPStatus(typed nil AppError) = %d, want 200", got)
	}
}

func TestTypedNilAppErrorDoesNotHideSiblingError(t *testing.T) {
	var appErr *AppError
	err := Join(appErr, stderrors.New("real failure"))

	if got := Normalize(err); got == nil {
		t.Fatal("Normalize(joined typed nil AppError, real error) = nil, want original error")
	}
	if got := Code(err); got != Internal {
		t.Fatalf("Code(joined typed nil AppError, real error) = %q, want %q", got, Internal)
	}
	if got := ToHTTPStatus(err); got != 500 {
		t.Fatalf("ToHTTPStatus(joined typed nil AppError, real error) = %d, want 500", got)
	}
}

func TestTypedNilAppErrorDoesNotHideSiblingAppError(t *testing.T) {
	var nilAppErr *AppError
	err := Join(nilAppErr, NewCode(NotFound, "missing"))

	if got := Code(err); got != NotFound {
		t.Fatalf("Code(joined typed nil AppError, NotFound) = %q, want %q", got, NotFound)
	}
	if got := ToHTTPStatus(err); got != 404 {
		t.Fatalf("ToHTTPStatus(joined typed nil AppError, NotFound) = %d, want 404", got)
	}
}

func TestWrappedJoinedTypedNilAppErrorDoesNotHideSiblingAppError(t *testing.T) {
	var nilAppErr *AppError
	err := fmt.Errorf("wrap: %w", Join(nilAppErr, NewCode(NotFound, "missing")))

	if got := Code(err); got != NotFound {
		t.Fatalf("Code(wrapped joined typed nil AppError, NotFound) = %q, want %q", got, NotFound)
	}
	if got := ToHTTPStatus(err); got != 404 {
		t.Fatalf("ToHTTPStatus(wrapped joined typed nil AppError, NotFound) = %d, want 404", got)
	}
	if got := Normalize(err); got != err {
		t.Fatalf("Normalize(wrapped joined typed nil AppError, NotFound) = %#v, want original error", got)
	}
}

func TestCustomAsAppErrorIsRecognized(t *testing.T) {
	err := customAsAppError{appErr: NewCode(Conflict, "conflict")}

	if got := Code(err); got != Conflict {
		t.Fatalf("Code(custom As AppError) = %q, want %q", got, Conflict)
	}
	if got := Normalize(err); got != err {
		t.Fatalf("Normalize(custom As AppError) = %#v, want original error", got)
	}
}

func TestCustomAsAppErrorInJoinedChainSkipsTypedNilSibling(t *testing.T) {
	var nilAppErr *AppError
	err := Join(nilAppErr, customAsAppError{appErr: NewCode(NotFound, "missing")})

	if got := Code(err); got != NotFound {
		t.Fatalf("Code(joined typed nil AppError, custom As AppError) = %q, want %q", got, NotFound)
	}
	if got := Normalize(err); got != err {
		t.Fatalf("Normalize(joined typed nil AppError, custom As AppError) = %#v, want original error", got)
	}
}
