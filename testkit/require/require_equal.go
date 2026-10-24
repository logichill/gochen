package require

import (
	"gochen/testkit/assert"
)

// Equal 断言 expected 与 actual 相等，否则立即终止测试。
func Equal(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Equal(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// Equalf 断言 expected 与 actual 相等，支持格式化消息，否则立即终止测试。
func Equalf(t TestingT, expected, actual any, format string, args ...any) {
	t.Helper()
	if !assert.Equalf(t, expected, actual, format, args...) {
		t.FailNow()
	}
}

// NotEqual 断言 expected 与 actual 不相等，否则立即终止测试。
func NotEqual(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotEqual(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// NotEqualf 断言 expected 与 actual 不相等，支持格式化消息，否则立即终止测试。
func NotEqualf(t TestingT, expected, actual any, format string, args ...any) {
	t.Helper()
	if !assert.NotEqualf(t, expected, actual, format, args...) {
		t.FailNow()
	}
}

// EqualValues 断言 expected 与 actual 值相等，否则立即终止测试。
func EqualValues(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !assert.EqualValues(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// EqualValuesf 断言 expected 与 actual 值相等，支持格式化消息，否则立即终止测试。
func EqualValuesf(t TestingT, expected, actual any, format string, args ...any) {
	t.Helper()
	if !assert.EqualValuesf(t, expected, actual, format, args...) {
		t.FailNow()
	}
}

// Same 断言 expected 与 actual 指向同一个地址，否则立即终止测试。
func Same(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Same(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// NotSame 断言 expected 与 actual 不指向同一个地址，否则立即终止测试。
func NotSame(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotSame(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// IsType 断言类型相同，否则立即终止测试。
func IsType(t TestingT, expectedType, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.IsType(t, expectedType, object, msgAndArgs...) {
		t.FailNow()
	}
}
