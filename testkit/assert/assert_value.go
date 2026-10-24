package assert

import (
	"reflect"
)

func isNil(object any) bool {
	if object == nil {
		return true
	}
	val := reflect.ValueOf(object)
	switch val.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return val.IsNil()
	default:
		return false
	}
}

func isEmpty(object any) bool {
	if isNil(object) {
		return true
	}
	val := reflect.ValueOf(object)
	switch val.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return val.Len() == 0
	default:
		return isZero(object)
	}
}

func isZero(value any) bool {
	if value == nil {
		return true
	}
	return reflect.ValueOf(value).IsZero()
}

// True 断言 value 为 true。
func True(t TestingT, value bool, msgAndArgs ...any) bool {
	t.Helper()
	if !value {
		t.Errorf("Should be true%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Truef 断言 value 为 true，支持格式化消息。
func Truef(t TestingT, value bool, format string, args ...any) bool {
	t.Helper()
	return True(t, value, append([]any{format}, args...)...)
}

// False 断言 value 为 false。
func False(t TestingT, value bool, msgAndArgs ...any) bool {
	t.Helper()
	if value {
		t.Errorf("Should be false%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Falsef 断言 value 为 false，支持格式化消息。
func Falsef(t TestingT, value bool, format string, args ...any) bool {
	t.Helper()
	return False(t, value, append([]any{format}, args...)...)
}

// Nil 断言 object 为 nil。
func Nil(t TestingT, object any, msgAndArgs ...any) bool {
	t.Helper()
	if !isNil(object) {
		t.Errorf("Expected nil, but got: %#v%s", object, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Nilf 断言 object 为 nil，支持格式化消息。
func Nilf(t TestingT, object any, format string, args ...any) bool {
	t.Helper()
	return Nil(t, object, append([]any{format}, args...)...)
}

// NotNil 断言 object 不为 nil。
func NotNil(t TestingT, object any, msgAndArgs ...any) bool {
	t.Helper()
	if isNil(object) {
		t.Errorf("Expected not nil, but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotNilf 断言 object 不为 nil，支持格式化消息。
func NotNilf(t TestingT, object any, format string, args ...any) bool {
	t.Helper()
	return NotNil(t, object, append([]any{format}, args...)...)
}

// Empty 断言 object 为空（nil、长度为 0 或零值）。
func Empty(t TestingT, object any, msgAndArgs ...any) bool {
	t.Helper()
	if !isEmpty(object) {
		t.Errorf("Should be empty, but got: %#v%s", object, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Emptyf 断言 object 为空，支持格式化消息。
func Emptyf(t TestingT, object any, format string, args ...any) bool {
	t.Helper()
	return Empty(t, object, append([]any{format}, args...)...)
}

// NotEmpty 断言 object 不为空。
func NotEmpty(t TestingT, object any, msgAndArgs ...any) bool {
	t.Helper()
	if isEmpty(object) {
		t.Errorf("Should NOT be empty, but was: %#v%s", object, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotEmptyf 断言 object 不为空，支持格式化消息。
func NotEmptyf(t TestingT, object any, format string, args ...any) bool {
	t.Helper()
	return NotEmpty(t, object, append([]any{format}, args...)...)
}

// Zero 断言 value 为零值。
func Zero(t TestingT, value any, msgAndArgs ...any) bool {
	t.Helper()
	if !isZero(value) {
		t.Errorf("Should be zero, but got: %#v%s", value, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Zerof 断言 value 为零值，支持格式化消息。
func Zerof(t TestingT, value any, format string, args ...any) bool {
	t.Helper()
	return Zero(t, value, append([]any{format}, args...)...)
}

// NotZero 断言 value 不为零值。
func NotZero(t TestingT, value any, msgAndArgs ...any) bool {
	t.Helper()
	if isZero(value) {
		t.Errorf("Should NOT be zero, but was zero value%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotZerof 断言 value 不为零值，支持格式化消息。
func NotZerof(t TestingT, value any, format string, args ...any) bool {
	t.Helper()
	return NotZero(t, value, append([]any{format}, args...)...)
}
