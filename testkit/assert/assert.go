// Package assert 提供基于 Go 标准库实现的纯净测试断言函数。
//
// 零外部依赖，兼容常用断言语法。
package assert

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// TestingT 是 testing.T 或 testing.B 的最小接口抽象。
type TestingT interface {
	Errorf(format string, args ...any)
	FailNow()
	Helper()
}

// AnError 是用于测试的通用错误占位变量。
var AnError = errors.New("assert.AnError general error for testing")

func formatMsg(msgAndArgs ...any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}
	if msg, ok := msgAndArgs[0].(string); ok {
		if len(msgAndArgs) == 1 {
			return ": " + msg
		}
		return ": " + fmt.Sprintf(msg, msgAndArgs[1:]...)
	}
	return ": " + fmt.Sprint(msgAndArgs...)
}

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

// NoError 断言 err 为 nil。
func NoError(t TestingT, err error, msgAndArgs ...any) bool {
	t.Helper()
	if err != nil {
		t.Errorf("Received unexpected error: %v%s", err, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NoErrorf 断言 err 为 nil，支持格式化消息。
func NoErrorf(t TestingT, err error, format string, args ...any) bool {
	t.Helper()
	if err != nil {
		t.Errorf("Received unexpected error: %v: "+format, append([]any{err}, args...)...)
		return false
	}
	return true
}

// Error 断言 err 不为 nil。
func Error(t TestingT, err error, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Errorf 断言 err 不为 nil，支持格式化消息。
func Errorf(t TestingT, err error, format string, args ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil: "+format, args...)
		return false
	}
	return true
}

// ErrorIs 断言 err 包含 target（通过 errors.Is）。
func ErrorIs(t TestingT, err, target error, msgAndArgs ...any) bool {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("Target error should be in err chain:\nexpected: %v\ngot: %v%s", target, err, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorIsf 断言 err 包含 target，支持格式化消息。
func ErrorIsf(t TestingT, err, target error, format string, args ...any) bool {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("Target error should be in err chain:\nexpected: %v\ngot: %v: "+format, append([]any{target, err}, args...)...)
		return false
	}
	return true
}

// ErrorAs 断言 err 可以转换为 target（通过 errors.As）。
func ErrorAs(t TestingT, err error, target any, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	if !errors.As(err, target) {
		t.Errorf("Should be able to cast %v to %T%s", err, target, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorContains 断言 err 的错误信息包含指定子串。
func ErrorContains(t TestingT, err error, contains string, msgAndArgs ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil%s", formatMsg(msgAndArgs...))
		return false
	}
	if !strings.Contains(err.Error(), contains) {
		t.Errorf("Error %q does not contain %q%s", err.Error(), contains, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ErrorContainsf 断言 err 的错误信息包含指定子串，支持格式化消息。
func ErrorContainsf(t TestingT, err error, contains string, format string, args ...any) bool {
	t.Helper()
	if err == nil {
		t.Errorf("An error is expected but got nil: "+format, args...)
		return false
	}
	if !strings.Contains(err.Error(), contains) {
		t.Errorf("Error %q does not contain %q: "+format, append([]any{err.Error(), contains}, args...)...)
		return false
	}
	return true
}

// Equal 断言 expected 与 actual 相等（通过 reflect.DeepEqual 或直接比较）。
func Equal(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) {
		return true
	}
	if isNil(expected) || isNil(actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// Equalf 断言 expected 与 actual 相等，支持格式化消息。
func Equalf(t TestingT, expected, actual any, format string, args ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) {
		return true
	}
	if isNil(expected) || isNil(actual) || !reflect.DeepEqual(expected, actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v: "+format, append([]any{expected, actual}, args...)...)
		return false
	}
	return true
}

// NotEqual 断言 expected 与 actual 不相等。
func NotEqual(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) {
		t.Errorf("Should not be equal: %#v%s", actual, formatMsg(msgAndArgs...))
		return false
	}
	if !isNil(expected) && !isNil(actual) && reflect.DeepEqual(expected, actual) {
		t.Errorf("Should not be equal: %#v%s", actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotEqualf 断言 expected 与 actual 不相等，支持格式化消息。
func NotEqualf(t TestingT, expected, actual any, format string, args ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) || (!isNil(expected) && !isNil(actual) && reflect.DeepEqual(expected, actual)) {
		t.Errorf("Should not be equal: %#v: "+format, append([]any{actual}, args...)...)
		return false
	}
	return true
}

// EqualValues 断言 expected 与 actual 在类型转换后值相等。
func EqualValues(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) {
		return true
	}
	if isNil(expected) || isNil(actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	expVal := reflect.ValueOf(expected)
	actVal := reflect.ValueOf(actual)
	if expVal.Type().ConvertibleTo(actVal.Type()) {
		converted := expVal.Convert(actVal.Type()).Interface()
		if reflect.DeepEqual(converted, actual) {
			return true
		}
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// EqualValuesf 断言 expected 与 actual 在类型转换后值相等，支持格式化消息。
func EqualValuesf(t TestingT, expected, actual any, format string, args ...any) bool {
	t.Helper()
	if EqualValues(t, expected, actual) {
		return true
	}
	t.Errorf(format, args...)
	return false
}

// Same 断言 expected 与 actual 指向同一个内存地址。
func Same(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if reflect.ValueOf(expected).Pointer() != reflect.ValueOf(actual).Pointer() {
		t.Errorf("Not same pointer:\nexpected: %p\nactual  : %p%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotSame 断言 expected 与 actual 不指向同一个内存地址。
func NotSame(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if reflect.ValueOf(expected).Pointer() == reflect.ValueOf(actual).Pointer() {
		t.Errorf("Should not be same pointer: %p%s", actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
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
	if !value {
		t.Errorf("Should be true: "+format, args...)
		return false
	}
	return true
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
	if value {
		t.Errorf("Should be false: "+format, args...)
		return false
	}
	return true
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
	if !isNil(object) {
		t.Errorf("Expected nil, but got: %#v: "+format, append([]any{object}, args...)...)
		return false
	}
	return true
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
	if isNil(object) {
		t.Errorf("Expected not nil, but got nil: "+format, args...)
		return false
	}
	return true
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
	if !isEmpty(object) {
		t.Errorf("Should be empty, but got: %#v: "+format, append([]any{object}, args...)...)
		return false
	}
	return true
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
	if isEmpty(object) {
		t.Errorf("Should NOT be empty: "+format, args...)
		return false
	}
	return true
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
	if !isZero(value) {
		t.Errorf("Should be zero, but got: %#v: "+format, append([]any{value}, args...)...)
		return false
	}
	return true
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
	if isZero(value) {
		t.Errorf("Should NOT be zero: "+format, args...)
		return false
	}
	return true
}

// Len 断言集合对象（slice, map, array, chan, string）的长度等于 length。
func Len(t TestingT, object any, length int, msgAndArgs ...any) bool {
	t.Helper()
	if object == nil {
		t.Errorf("Object is nil, cannot get length%s", formatMsg(msgAndArgs...))
		return false
	}
	val := reflect.ValueOf(object)
	switch val.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		if val.Len() != length {
			t.Errorf("Length mismatch:\nexpected: %d\nactual  : %d%s", length, val.Len(), formatMsg(msgAndArgs...))
			return false
		}
		return true
	default:
		t.Errorf("Object of type %T does not have length%s", object, formatMsg(msgAndArgs...))
		return false
	}
}

// Lenf 断言集合长度等于 length，支持格式化消息。
func Lenf(t TestingT, object any, length int, format string, args ...any) bool {
	t.Helper()
	if !Len(t, object, length) {
		t.Errorf(format, args...)
		return false
	}
	return true
}

// Contains 断言 s 包含 contains。
func Contains(t TestingT, s, contains any, msgAndArgs ...any) bool {
	t.Helper()
	if str, ok := s.(string); ok {
		if sub, ok := contains.(string); ok {
			if !strings.Contains(str, sub) {
				t.Errorf("%q does not contain %q%s", str, sub, formatMsg(msgAndArgs...))
				return false
			}
			return true
		}
	}
	val := reflect.ValueOf(s)
	if val.IsValid() {
		switch val.Kind() {
		case reflect.Slice, reflect.Array:
			for i := 0; i < val.Len(); i++ {
				if reflect.DeepEqual(val.Index(i).Interface(), contains) {
					return true
				}
			}
			t.Errorf("%#v does not contain %#v%s", s, contains, formatMsg(msgAndArgs...))
			return false
		case reflect.Map:
			keyVal := reflect.ValueOf(contains)
			if keyVal.IsValid() && keyVal.Type().AssignableTo(val.Type().Key()) {
				if val.MapIndex(keyVal).IsValid() {
					return true
				}
			}
			t.Errorf("%#v does not contain key %#v%s", s, contains, formatMsg(msgAndArgs...))
			return false
		}
	}
	t.Errorf("Cannot check contains for type %T%s", s, formatMsg(msgAndArgs...))
	return false
}

// Containsf 断言 s 包含 contains，支持格式化消息。
func Containsf(t TestingT, s, contains any, format string, args ...any) bool {
	t.Helper()
	if !Contains(t, s, contains) {
		t.Errorf(format, args...)
		return false
	}
	return true
}

// NotContains 断言 s 不包含 contains。
func NotContains(t TestingT, s, contains any, msgAndArgs ...any) bool {
	t.Helper()
	if str, ok := s.(string); ok {
		if sub, ok := contains.(string); ok {
			if strings.Contains(str, sub) {
				t.Errorf("%q should not contain %q%s", str, sub, formatMsg(msgAndArgs...))
				return false
			}
			return true
		}
	}
	val := reflect.ValueOf(s)
	if val.IsValid() {
		switch val.Kind() {
		case reflect.Slice, reflect.Array:
			for i := 0; i < val.Len(); i++ {
				if reflect.DeepEqual(val.Index(i).Interface(), contains) {
					t.Errorf("%#v should not contain %#v%s", s, contains, formatMsg(msgAndArgs...))
					return false
				}
			}
			return true
		case reflect.Map:
			keyVal := reflect.ValueOf(contains)
			if keyVal.IsValid() && keyVal.Type().AssignableTo(val.Type().Key()) {
				if val.MapIndex(keyVal).IsValid() {
					t.Errorf("%#v should not contain key %#v%s", s, contains, formatMsg(msgAndArgs...))
					return false
				}
			}
			return true
		}
	}
	return true
}

// NotContainsf 断言 s 不包含 contains，支持格式化消息。
func NotContainsf(t TestingT, s, contains any, format string, args ...any) bool {
	t.Helper()
	if !NotContains(t, s, contains) {
		t.Errorf(format, args...)
		return false
	}
	return true
}

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
	if v1.CanFloat() && v2.CanFloat() {
		f1, f2 := v1.Float(), v2.Float()
		if f1 < f2 {
			return -1, true
		} else if f1 > f2 {
			return 1, true
		}
		return 0, true
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
	if !ok1 || !ok2 {
		t.Errorf("Cannot convert values to float64 for InDelta%s", formatMsg(msgAndArgs...))
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

// JSONEq 断言两个 JSON 字符串反序列化后的语义结构相等。
func JSONEq(t TestingT, expected, actual string, msgAndArgs ...any) bool {
	t.Helper()
	var expObj, actObj any
	if err := json.Unmarshal([]byte(expected), &expObj); err != nil {
		t.Errorf("Expected string is not valid JSON: %v%s", err, formatMsg(msgAndArgs...))
		return false
	}
	if err := json.Unmarshal([]byte(actual), &actObj); err != nil {
		t.Errorf("Actual string is not valid JSON: %v%s", err, formatMsg(msgAndArgs...))
		return false
	}
	if !reflect.DeepEqual(expObj, actObj) {
		t.Errorf("JSON mismatch:\nexpected: %s\nactual  : %s%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// ElementsMatch 断言两个 slice 包含相同的元素（不考虑顺序）。
func ElementsMatch(t TestingT, listA, listB any, msgAndArgs ...any) bool {
	t.Helper()
	valA := reflect.ValueOf(listA)
	valB := reflect.ValueOf(listB)
	if !valA.IsValid() || !valB.IsValid() || valA.Kind() != reflect.Slice || valB.Kind() != reflect.Slice {
		t.Errorf("ElementsMatch requires two slices%s", formatMsg(msgAndArgs...))
		return false
	}
	if valA.Len() != valB.Len() {
		t.Errorf("ElementsMatch length mismatch: %d != %d%s", valA.Len(), valB.Len(), formatMsg(msgAndArgs...))
		return false
	}
	visited := make([]bool, valB.Len())
	for i := 0; i < valA.Len(); i++ {
		itemA := valA.Index(i).Interface()
		found := false
		for j := 0; j < valB.Len(); j++ {
			if !visited[j] && reflect.DeepEqual(itemA, valB.Index(j).Interface()) {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Elements do not match:\nlistA: %#v\nlistB: %#v%s", listA, listB, formatMsg(msgAndArgs...))
			return false
		}
	}
	return true
}

// IsType 断言 object 的动态类型与 expectedType 相同。
func IsType(t TestingT, expectedType, object any, msgAndArgs ...any) bool {
	t.Helper()
	expT := reflect.TypeOf(expectedType)
	actT := reflect.TypeOf(object)
	if expT != actT {
		t.Errorf("Type mismatch:\nexpected: %v\nactual  : %v%s", expT, actT, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

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
