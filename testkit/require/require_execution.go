package require

import (
	"time"

	"gochen/testkit/assert"
)

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
