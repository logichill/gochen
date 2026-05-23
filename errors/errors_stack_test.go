package errors

import (
	"fmt"
	"strings"
	"testing"
)

type typedStdCompatError struct {
	message string
}

func (e *typedStdCompatError) Error() string {
	return e.message
}

func TestNewCode_CapturesStack_OnlyFor5xx(t *testing.T) {
	err := NewCode(Internal, "boom")
	if err == nil {
		t.Fatal("expected err")
	}
	if !strings.Contains(err.Error(), "INTERNAL_ERROR") {
		t.Fatalf("unexpected Error() output: %s", err.Error())
	}

	stack, ok := err.Details()["stack"].(string)
	if !ok || strings.TrimSpace(stack) == "" {
		t.Fatalf("expected stack in details, got: %#v", err.Details())
	}
	if !strings.Contains(stack, "TestNewCode_CapturesStack_OnlyFor5xx") {
		t.Fatalf("expected stack to contain test function name, got:\n%s", stack)
	}

	err = NewCode(InvalidInput, "bad request")
	if err == nil {
		t.Fatal("expected err")
	}
	if _, ok := err.Details()["stack"]; ok {
		t.Fatalf("did not expect stack for 4xx error, got: %#v", err.Details())
	}
}

func TestWrap_ReusesExistingStack(t *testing.T) {
	inner := NewCode(Database, "db failed")
	innerStack, ok := inner.Details()["stack"].(string)
	if !ok || strings.TrimSpace(innerStack) == "" {
		t.Fatalf("expected stack in inner details, got: %#v", inner.Details())
	}

	outer := Wrap(inner, Internal, "outer")
	if outer == nil {
		t.Fatal("expected outer")
	}
	outerStack, ok := outer.Details()["stack"].(string)
	if !ok || strings.TrimSpace(outerStack) == "" {
		t.Fatalf("expected stack in outer details, got: %#v", outer.Details())
	}

	if strings.TrimSpace(outerStack) != strings.TrimSpace(innerStack) {
		t.Fatalf("expected outer stack to reuse inner stack.\ninner:\n%s\nouter:\n%s", innerStack, outerStack)
	}
}

func TestNew_MatchesStdlibContract(t *testing.T) {
	err := New("plain")
	if err == nil || err.Error() != "plain" {
		t.Fatalf("unexpected std-style error: %#v", err)
	}
}

func TestStdCompatibilityHelpers(t *testing.T) {
	root := New("root")
	err := Wrap(root, NotFound, "wrapped")
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !Is(err, NotFound) {
		t.Fatal("expected errors.Is to match ErrorCode target")
	}
	if !Is(err, root) {
		t.Fatal("expected errors.Is to match wrapped cause")
	}
	var appErr *AppError
	if !As(err, &appErr) || appErr == nil || appErr.Code() != NotFound {
		t.Fatalf("expected errors.As to extract AppError, got %#v", appErr)
	}
	joined := Join(err, New("extra"))
	if joined == nil || !Is(joined, NotFound) {
		t.Fatalf("expected joined error to preserve ErrorCode matching, got %#v", joined)
	}

	typed := &typedStdCompatError{message: "typed"}
	typedErr := Wrap(typed, Internal, "typed wrapper")
	got, ok := AsType[*typedStdCompatError](typedErr)
	if !ok || got != typed {
		t.Fatalf("expected AsType to extract typed error, got %#v, %v", got, ok)
	}
}

func TestErrUnsupported_BridgesStdlibAndGochenCode(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("expected ErrUnsupported")
	}
	if Code(ErrUnsupported) != Unsupported {
		t.Fatalf("expected stdlib ErrUnsupported to map to Unsupported, got %q", Code(ErrUnsupported))
	}

	normalized := Normalize(ErrUnsupported)
	var appErr *AppError
	if !As(normalized, &appErr) || appErr == nil {
		t.Fatalf("expected Normalize to wrap ErrUnsupported as AppError, got %#v", normalized)
	}
	if appErr.Code() != Unsupported {
		t.Fatalf("expected Unsupported code, got %q", appErr.Code())
	}
	if !Is(normalized, ErrUnsupported) {
		t.Fatal("expected normalized error to preserve ErrUnsupported matching")
	}
	if ToHTTPStatus(normalized) != 400 {
		t.Fatalf("expected Unsupported to map to 400, got %d", ToHTTPStatus(normalized))
	}
	if _, ok := appErr.Details()["stack"]; ok {
		t.Fatalf("did not expect stack for Unsupported error, got: %#v", appErr.Details())
	}

	gochenErr := NewCode(Unsupported, "not supported")
	if !Is(gochenErr, ErrUnsupported) {
		t.Fatal("expected gochen Unsupported code to match ErrUnsupported")
	}
}

func TestNormalize_WrappedErrUnsupported(t *testing.T) {
	err := fmt.Errorf("adapter does not support stream: %w", ErrUnsupported)
	normalized := Normalize(err)

	var appErr *AppError
	if !As(normalized, &appErr) || appErr == nil {
		t.Fatalf("expected wrapped ErrUnsupported to normalize as AppError, got %#v", normalized)
	}
	if appErr.Code() != Unsupported {
		t.Fatalf("expected Unsupported code, got %q", appErr.Code())
	}
	if !Is(normalized, ErrUnsupported) {
		t.Fatal("expected normalized wrapped error to match ErrUnsupported")
	}
	if !strings.Contains(normalized.Error(), "adapter does not support stream") {
		t.Fatalf("expected original message to be preserved, got %q", normalized.Error())
	}
}

func TestAppErrorDetails_AreTopLevelCopies(t *testing.T) {
	err := NewCode(InvalidInput, "invalid").WithContext("field", "name")

	details := err.Details()
	details["field"] = "mutated"
	if got := err.Details()["field"]; got != "name" {
		t.Fatalf("expected Details to return a copy, got field=%#v", got)
	}

	withContext := err.WithContext("reason", "empty")
	if _, ok := err.Details()["reason"]; ok {
		t.Fatal("expected WithContext to leave original details unchanged")
	}
	if got := withContext.Details()["reason"]; got != "empty" {
		t.Fatalf("expected WithContext result to include reason, got %#v", got)
	}

	extra := map[string]any{"hint": "trim spaces"}
	withDetails := err.WithDetails(extra)
	extra["hint"] = "mutated"
	if got := withDetails.Details()["hint"]; got != "trim spaces" {
		t.Fatalf("expected WithDetails to copy input map, got hint=%#v", got)
	}
}
