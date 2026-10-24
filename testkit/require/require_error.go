package require

import (
	"gochen/testkit/assert"
)

// NoError 断言 err 为 nil，否则立即终止测试。
func NoError(t TestingT, err error, msgAndArgs ...any) {
	t.Helper()
	if !assert.NoError(t, err, msgAndArgs...) {
		t.FailNow()
	}
}

// NoErrorf 断言 err 为 nil，支持格式化消息，否则立即终止测试。
func NoErrorf(t TestingT, err error, format string, args ...any) {
	t.Helper()
	if !assert.NoErrorf(t, err, format, args...) {
		t.FailNow()
	}
}

// Error 断言 err 不为 nil，否则立即终止测试。
func Error(t TestingT, err error, msgAndArgs ...any) {
	t.Helper()
	if !assert.Error(t, err, msgAndArgs...) {
		t.FailNow()
	}
}

// Errorf 断言 err 不为 nil，支持格式化消息，否则立即终止测试。
func Errorf(t TestingT, err error, format string, args ...any) {
	t.Helper()
	if !assert.Errorf(t, err, format, args...) {
		t.FailNow()
	}
}

// ErrorIs 断言 err 包含 target，否则立即终止测试。
func ErrorIs(t TestingT, err, target error, msgAndArgs ...any) {
	t.Helper()
	if !assert.ErrorIs(t, err, target, msgAndArgs...) {
		t.FailNow()
	}
}

// ErrorIsf 断言 err 包含 target，支持格式化消息，否则立即终止测试。
func ErrorIsf(t TestingT, err, target error, format string, args ...any) {
	t.Helper()
	if !assert.ErrorIsf(t, err, target, format, args...) {
		t.FailNow()
	}
}

// ErrorAs 断言 err 可以转换为 target，否则立即终止测试。
func ErrorAs(t TestingT, err error, target any, msgAndArgs ...any) {
	t.Helper()
	if !assert.ErrorAs(t, err, target, msgAndArgs...) {
		t.FailNow()
	}
}

// ErrorContains 断言 err 包含指定字符串，否则立即终止测试。
func ErrorContains(t TestingT, err error, contains string, msgAndArgs ...any) {
	t.Helper()
	if !assert.ErrorContains(t, err, contains, msgAndArgs...) {
		t.FailNow()
	}
}

// ErrorContainsf 断言 err 包含指定字符串，支持格式化消息，否则立即终止测试。
func ErrorContainsf(t TestingT, err error, contains string, format string, args ...any) {
	t.Helper()
	if !assert.ErrorContainsf(t, err, contains, format, args...) {
		t.FailNow()
	}
}
