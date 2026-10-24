package require

import (
	"time"

	"gochen/testkit/assert"
)

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
