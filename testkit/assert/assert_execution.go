package assert

import (
	"reflect"
	"time"
)

// NotPanics 断言 f 执行时不产生 panic。
func NotPanics(t TestingT, f func(), msgAndArgs ...any) bool {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("func should not panic, but did: %v%s", r, formatMsg(msgAndArgs...))
		}
	}()
	f()
	return true
}

// Panics 断言 f 执行时会产生 panic。
func Panics(t TestingT, f func(), msgAndArgs ...any) bool {
	t.Helper()
	var didPanic bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		f()
	}()
	if !didPanic {
		t.Errorf("func should panic, but did not%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// PanicsWithValue 断言 f 执行时产生 panic，且 panic 的值与 expected 相等。
func PanicsWithValue(t TestingT, expected any, f func(), msgAndArgs ...any) bool {
	t.Helper()
	var panicVal any
	var didPanic bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicVal = r
			}
		}()
		f()
	}()
	if !didPanic {
		t.Errorf("func should panic, but did not%s", formatMsg(msgAndArgs...))
		return false
	}
	if !reflect.DeepEqual(expected, panicVal) {
		t.Errorf("Panic value mismatch:\nexpected: %#v\ngot     : %#v%s", expected, panicVal, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Eventually 在 waitFor 超时时间内按 tick 周期轮询 condition，直到返回 true。
func Eventually(t TestingT, condition func() bool, waitFor time.Duration, tick time.Duration, msgAndArgs ...any) bool {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(tick)
	}
	t.Errorf("Condition was not satisfied within %v%s", waitFor, formatMsg(msgAndArgs...))
	return false
}
