package require

import (
	"gochen/testkit/assert"
)

// Len 断言集合长度等于 length，否则立即终止测试。
func Len(t TestingT, object any, length int, msgAndArgs ...any) {
	t.Helper()
	if !assert.Len(t, object, length, msgAndArgs...) {
		t.FailNow()
	}
}

// Lenf 断言集合长度等于 length，支持格式化消息，否则立即终止测试。
func Lenf(t TestingT, object any, length int, format string, args ...any) {
	t.Helper()
	if !assert.Lenf(t, object, length, format, args...) {
		t.FailNow()
	}
}

// Contains 断言 s 包含 contains，否则立即终止测试。
func Contains(t TestingT, s, contains any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Contains(t, s, contains, msgAndArgs...) {
		t.FailNow()
	}
}

// Containsf 断言 s 包含 contains，支持格式化消息，否则立即终止测试。
func Containsf(t TestingT, s, contains any, format string, args ...any) {
	t.Helper()
	if !assert.Containsf(t, s, contains, format, args...) {
		t.FailNow()
	}
}

// NotContains 断言 s 不包含 contains，否则立即终止测试。
func NotContains(t TestingT, s, contains any, msgAndArgs ...any) {
	t.Helper()
	if !assert.NotContains(t, s, contains, msgAndArgs...) {
		t.FailNow()
	}
}

// NotContainsf 断言 s 不包含 contains，支持格式化消息，否则立即终止测试。
func NotContainsf(t TestingT, s, contains any, format string, args ...any) {
	t.Helper()
	if !assert.NotContainsf(t, s, contains, format, args...) {
		t.FailNow()
	}
}

// JSONEq 断言两个 JSON 字符串语义相等，否则立即终止测试。
func JSONEq(t TestingT, expected, actual string, msgAndArgs ...any) {
	t.Helper()
	if !assert.JSONEq(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}

// ElementsMatch 断言两个 slice 元素相同，否则立即终止测试。
func ElementsMatch(t TestingT, listA, listB any, msgAndArgs ...any) {
	t.Helper()
	if !assert.ElementsMatch(t, listA, listB, msgAndArgs...) {
		t.FailNow()
	}
}
