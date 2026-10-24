package require

import (
	"gochen/testkit/assert"
)

// True 断言 value 为 true，否则立即终止测试。
func True(t TestingT, value bool, msgAndArgs ...any) {
	t.Helper()
	if !assert.True(t, value, msgAndArgs...) {
		t.FailNow()
	}
}

// Truef 断言 value 为 true，支持格式化消息，否则立即终止测试。
func Truef(t TestingT, value bool, format string, args ...any) {
	t.Helper()
	if !assert.Truef(t, value, format, args...) {
		t.FailNow()
	}
}

// False 断言 value 为 false，否则立即终止测试。
func False(t TestingT, value bool, msgAndArgs ...any) {
	t.Helper()
	if !assert.False(t, value, msgAndArgs...) {
		t.FailNow()
	}
}

// Falsef 断言 value 为 false，支持格式化消息，否则立即终止测试。
func Falsef(t TestingT, value bool, format string, args ...any) {
	t.Helper()
	if !assert.Falsef(t, value, format, args...) {
		t.FailNow()
	}
}

// Nil 断言 object 为 nil，否则立即终止测试。
func Nil(t TestingT, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Nil(t, object, msgAndArgs...) {
		t.FailNow()
	}
}

// Nilf 断言 object 为 nil，支持格式化消息，否则立即终止测试。
func Nilf(t TestingT, object any, format string, args ...any) {
	t.Helper()
	if !assert.Nilf(t, object, format, args...) {
		t.FailNow()
	}
}

// NotNil 断言 object 不为 nil，否则立即终止测试。
func NotNil(t TestingT, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotNil(t, object, msgAndArgs...) {
		t.FailNow()
	}
}

// NotNilf 断言 object 不为 nil，支持格式化消息，否则立即终止测试。
func NotNilf(t TestingT, object any, format string, args ...any) {
	t.Helper()
	if !assert.NotNilf(t, object, format, args...) {
		t.FailNow()
	}
}

// Empty 断言 object 为空，否则立即终止测试。
func Empty(t TestingT, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Empty(t, object, msgAndArgs...) {
		t.FailNow()
	}
}

// Emptyf 断言 object 为空，支持格式化消息，否则立即终止测试。
func Emptyf(t TestingT, object any, format string, args ...any) {
	t.Helper()
	if !assert.Emptyf(t, object, format, args...) {
		t.FailNow()
	}
}

// NotEmpty 断言 object 不为空，否则立即终止测试。
func NotEmpty(t TestingT, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotEmpty(t, object, msgAndArgs...) {
		t.FailNow()
	}
}

// NotEmptyf 断言 object 不为空，支持格式化消息，否则立即终止测试。
func NotEmptyf(t TestingT, object any, format string, args ...any) {
	t.Helper()
	if !assert.NotEmptyf(t, object, format, args...) {
		t.FailNow()
	}
}

// Zero 断言 value 为零值，否则立即终止测试。
func Zero(t TestingT, value any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Zero(t, value, msgAndArgs...) {
		t.FailNow()
	}
}

// Zerof 断言 value 为零值，支持格式化消息，否则立即终止测试。
func Zerof(t TestingT, value any, format string, args ...any) {
	t.Helper()
	if !assert.Zerof(t, value, format, args...) {
		t.FailNow()
	}
}

// NotZero 断言 value 不为零值，否则立即终止测试。
func NotZero(t TestingT, value any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotZero(t, value, msgAndArgs...) {
		t.FailNow()
	}
}

// NotZerof 断言 value 不为零值，支持格式化消息，否则立即终止测试。
func NotZerof(t TestingT, value any, format string, args ...any) {
	t.Helper()
	if !assert.NotZerof(t, value, format, args...) {
		t.FailNow()
	}
}
