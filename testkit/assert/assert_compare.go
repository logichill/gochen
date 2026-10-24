package assert

import (
	"math"
	"reflect"
	"time"
)

func compareNumbers(e1, e2 any) (cmp int, ok bool) {
	v1 := reflect.ValueOf(e1)
	v2 := reflect.ValueOf(e2)
	if v1.CanInt() && v2.CanInt() {
		i1, i2 := v1.Int(), v2.Int()
		if i1 < i2 {
			return -1, true
		} else if i1 > i2 {
			return 1, true
		}
		return 0, true
	}
	if v1.CanUint() && v2.CanUint() {
		u1, u2 := v1.Uint(), v2.Uint()
		if u1 < u2 {
			return -1, true
		} else if u1 > u2 {
			return 1, true
		}
		return 0, true
	}
	if v1.CanUint() && v2.CanInt() {
		i2 := v2.Int()
		if i2 < 0 {
			return 1, true
		}
		u1, u2 := v1.Uint(), uint64(i2)
		if u1 < u2 {
			return -1, true
		} else if u1 > u2 {
			return 1, true
		}
		return 0, true
	}
	if v1.CanInt() && v2.CanUint() {
		i1 := v1.Int()
		if i1 < 0 {
			return -1, true
		}
		u1, u2 := uint64(i1), v2.Uint()
		if u1 < u2 {
			return -1, true
		} else if u1 > u2 {
			return 1, true
		}
		return 0, true
	}
	if v1.CanFloat() && v2.CanFloat() {
		f1, f2 := v1.Float(), v2.Float()
		if math.IsNaN(f1) || math.IsNaN(f2) {
			return 0, false
		}
		if f1 < f2 {
			return -1, true
		} else if f1 > f2 {
			return 1, true
		}
		return 0, true
	}
	if v1.CanFloat() || v2.CanFloat() {
		// 浮点数与整型/无符号整型交叉比较：统一归一化为 float64 做数学值比对。
		// 缺失此分支会导致 Greater(3.5, 3)、Less(2, 2.5)、Positive(float64(1.0)) 等断言全部误报失败。
		f1, ok1 := toFloat(e1)
		f2, ok2 := toFloat(e2)
		if ok1 && ok2 {
			if math.IsNaN(f1) || math.IsNaN(f2) {
				return 0, false
			}
			if f1 < f2 {
				return -1, true
			} else if f1 > f2 {
				return 1, true
			}
			return 0, true
		}
	}
	if t1, ok1 := e1.(time.Time); ok1 {
		if t2, ok2 := e2.(time.Time); ok2 {
			if t1.Before(t2) {
				return -1, true
			} else if t1.After(t2) {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

// Greater 断言 e1 > e2。
func Greater(t TestingT, e1, e2 any, msgAndArgs ...any) bool {
	t.Helper()
	cmp, ok := compareNumbers(e1, e2)
	if !ok || cmp <= 0 {
		t.Errorf("%#v should be greater than %#v%s", e1, e2, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// GreaterOrEqual 断言 e1 >= e2。
func GreaterOrEqual(t TestingT, e1, e2 any, msgAndArgs ...any) bool {
	t.Helper()
	cmp, ok := compareNumbers(e1, e2)
	if !ok || cmp < 0 {
		t.Errorf("%#v should be greater than or equal to %#v%s", e1, e2, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Less 断言 e1 < e2。
func Less(t TestingT, e1, e2 any, msgAndArgs ...any) bool {
	t.Helper()
	cmp, ok := compareNumbers(e1, e2)
	if !ok || cmp >= 0 {
		t.Errorf("%#v should be less than %#v%s", e1, e2, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// LessOrEqual 断言 e1 <= e2。
func LessOrEqual(t TestingT, e1, e2 any, msgAndArgs ...any) bool {
	t.Helper()
	cmp, ok := compareNumbers(e1, e2)
	if !ok || cmp > 0 {
		t.Errorf("%#v should be less than or equal to %#v%s", e1, e2, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Positive 断言 e > 0。
func Positive(t TestingT, e any, msgAndArgs ...any) bool {
	t.Helper()
	return Greater(t, e, 0, msgAndArgs...)
}

// WithinDuration 断言 expected 与 actual 时间差在 delta 以内。
func WithinDuration(t TestingT, expected, actual time.Time, delta time.Duration, msgAndArgs ...any) bool {
	t.Helper()
	diff := expected.Sub(actual)
	if diff < 0 {
		diff = -diff
	}
	if diff > delta {
		t.Errorf("Time difference %v is greater than delta %v:\nexpected: %v\nactual  : %v%s", diff, delta, expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// InDelta 断言 expected 与 actual 数值差在 delta 以内。
func InDelta(t TestingT, expected, actual any, delta float64, msgAndArgs ...any) bool {
	t.Helper()
	expF, ok1 := toFloat(expected)
	actF, ok2 := toFloat(actual)
	if !ok1 || !ok2 || math.IsNaN(expF) || math.IsNaN(actF) || math.IsNaN(delta) {
		t.Errorf("Cannot convert values to float64 or NaN encountered for InDelta%s", formatMsg(msgAndArgs...))
		return false
	}
	diff := expF - actF
	if diff < 0 {
		diff = -diff
	}
	if diff > delta {
		t.Errorf("Difference %f is greater than delta %f:\nexpected: %v\nactual  : %v%s", diff, delta, expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

func toFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	if rv.CanFloat() {
		return rv.Float(), true
	}
	if rv.CanInt() {
		return float64(rv.Int()), true
	}
	if rv.CanUint() {
		return float64(rv.Uint()), true
	}
	return 0, false
}
