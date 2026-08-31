// Package require 提供基于 assert 实现并在失败时立即终止测试的断言包。
//
// 零外部依赖，等同于 testing.T.Fatalf / FailNow 行为。
package require

import (
	"time"

	"gochen/testkit/assert"
)

// TestingT 抽象 testing.T 或 testing.B。
type TestingT = assert.TestingT

// AnError 通用测试错误占位。
var AnError = assert.AnError

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

// Greater 断言 e1 > e2，否则立即终止测试。
func Greater(t TestingT, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Greater(t, e1, e2, msgAndArgs...) {
		t.FailNow()
	}
}

// GreaterOrEqual 断言 e1 >= e2，否则立即终止测试。
func GreaterOrEqual(t TestingT, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	if !assert.GreaterOrEqual(t, e1, e2, msgAndArgs...) {
		t.FailNow()
	}
}

// Less 断言 e1 < e2，否则立即终止测试。
func Less(t TestingT, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Less(t, e1, e2, msgAndArgs...) {
		t.FailNow()
	}
}

// LessOrEqual 断言 e1 <= e2，否则立即终止测试。
func LessOrEqual(t TestingT, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	if !assert.LessOrEqual(t, e1, e2, msgAndArgs...) {
		t.FailNow()
	}
}

// Positive 断言 e > 0，否则立即终止测试。
func Positive(t TestingT, e any, msgAndArgs ...any) {
	t.Helper()
	if !assert.Positive(t, e, msgAndArgs...) {
		t.FailNow()
	}
}

// WithinDuration 断言时间差在 delta 以内，否则立即终止测试。
func WithinDuration(t TestingT, expected, actual time.Time, delta time.Duration, msgAndArgs ...any) {
	t.Helper()
	if !assert.WithinDuration(t, expected, actual, delta, msgAndArgs...) {
		t.FailNow()
	}
}

// InDelta 断言数值差在 delta 以内，否则立即终止测试。
func InDelta(t TestingT, expected, actual any, delta float64, msgAndArgs ...any) {
	t.Helper()
	if !assert.InDelta(t, expected, actual, delta, msgAndArgs...) {
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

// IsType 断言类型相同，否则立即终止测试。
func IsType(t TestingT, expectedType, object any, msgAndArgs ...any) {
	t.Helper()
	if !assert.IsType(t, expectedType, object, msgAndArgs...) {
		t.FailNow()
	}
}

// NotPanics 断言 f 不 panic，否则立即终止测试。
func NotPanics(t TestingT, f func(), msgAndArgs ...any) {
	t.Helper()
	if !assert.NotPanics(t, f, msgAndArgs...) {
		t.FailNow()
	}
}

// Panics 断言 f 会 panic，否则立即终止测试。
func Panics(t TestingT, f func(), msgAndArgs ...any) {
	t.Helper()
	if !assert.Panics(t, f, msgAndArgs...) {
		t.FailNow()
	}
}

// PanicsWithValue 断言 f 会 panic 且值相等，否则立即终止测试。
func PanicsWithValue(t TestingT, expected any, f func(), msgAndArgs ...any) {
	t.Helper()
	if !assert.PanicsWithValue(t, expected, f, msgAndArgs...) {
		t.FailNow()
	}
}

// Eventually 在指定时间内轮询条件，若未满足则立即终止测试。
func Eventually(t TestingT, condition func() bool, waitFor time.Duration, tick time.Duration, msgAndArgs ...any) {
	t.Helper()
	if !assert.Eventually(t, condition, waitFor, tick, msgAndArgs...) {
		t.FailNow()
	}
}
