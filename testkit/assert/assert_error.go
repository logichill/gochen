package assert

import (
	"errors"
	"strings"
)

// NoError 断言 err 为 nil。
func NoError(t TestingT, err error, msgAndArgs ...any) bool {
	t.Helper()
	if err != nil {
		t.Errorf("Received unexpected error: %v%s", err, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NoErrorf 断言 err 为 nil，支持格式化消息。
func NoErrorf(t TestingT, err error, format string, args ...any) bool {
	t.Helper()
	return NoError(t, err, append([]any{format}, args...)...)
}

// Error 断言 err 不为 nil。
func Error(t TestingT, err error, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Errorf 断言 err 不为 nil，支持格式化消息。
func Errorf(t TestingT, err error, format string, args ...any) bool {
	t.Helper()
	return Error(t, err, append([]any{format}, args...)...)
}

// ErrorIs 断言 err 包含 target（通过 errors.Is）。
func ErrorIs(t TestingT, err, target error, msgAndArgs ...any) bool {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("Target error should be in err chain:\nexpected: %v\ngot: %v%s", target, err, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorIsf 断言 err 包含 target，支持格式化消息。
func ErrorIsf(t TestingT, err, target error, format string, args ...any) bool {
	t.Helper()
	return ErrorIs(t, err, target, append([]any{format}, args...)...)
}

// ErrorAs 断言 err 可以转换为 target（通过 errors.As）。
func ErrorAs(t TestingT, err error, target any, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	if !errors.As(err, target) {
		t.Errorf("Should be able to cast %v to %T%s", err, target, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorContains 断言 err 的错误信息包含指定子串。
func ErrorContains(t TestingT, err error, contains string, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	if !strings.Contains(err.Error(), contains) {
		t.Errorf("Error %q does not contain %q%s", err.Error(), contains, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorContainsf 断言 err 的错误信息包含指定子串，支持格式化消息。
func ErrorContainsf(t TestingT, err error, contains string, format string, args ...any) bool {
	t.Helper()
	return ErrorContains(t, err, contains, append([]any{format}, args...)...)
}
