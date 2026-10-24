package assert

import (
	"math"
	"reflect"
)

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
	return Equal(t, expected, actual, append([]any{format}, args...)...)
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
	return NotEqual(t, expected, actual, append([]any{format}, args...)...)
}

// EqualValues 断言 expected 与 actual 在类型转换后值相等。
//
// 判等口径：仅接受双向可转换且往返一致的类型；数值按数学值比较，
// 负数与无符号类型不判等，超出整数/浮点精确域的交叉比较不判等。
// 该口径比旧版更严格，依赖宽口径转换的下游断言应改为显式类型转换。
func EqualValues(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	if isNil(expected) && isNil(actual) {
		return true
	}
	if isNil(expected) || isNil(actual) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	if !reflect.DeepEqual(expected, actual) &&
		!equalValuesConvertible(reflect.ValueOf(expected), reflect.ValueOf(actual)) {
		t.Errorf("Not equal:\nexpected: %#v\nactual  : %#v%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// equalValuesConvertible 判断两个非同类型值在类型转换后是否相等。
// 仅当转换在两个方向都可行且往返一致时才判等；数值类型按数学值比较，
// 禁止有符号负数与无符号类型的补码互换，也禁止单向类型强转放行。
func equalValuesConvertible(expVal, actVal reflect.Value) bool {
	if isNumericKind(expVal.Kind()) && isNumericKind(actVal.Kind()) {
		return numericValuesEqual(expVal, actVal)
	}
	if !expVal.Type().ConvertibleTo(actVal.Type()) || !actVal.Type().ConvertibleTo(expVal.Type()) {
		return false
	}
	converted := expVal.Convert(actVal.Type()).Interface()
	if !reflect.DeepEqual(converted, actVal.Interface()) {
		return false
	}
	back := actVal.Convert(expVal.Type()).Interface()
	return reflect.DeepEqual(back, expVal.Interface())
}

func isNumericKind(k reflect.Kind) bool {
	return isSignedIntKind(k) || isUnsignedIntKind(k) || k == reflect.Float32 || k == reflect.Float64
}

func isSignedIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func isUnsignedIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

// numericValuesEqual 按数学值比较两个数值型反射值。
func numericValuesEqual(a, b reflect.Value) bool {
	aFloat := a.Kind() == reflect.Float32 || a.Kind() == reflect.Float64
	bFloat := b.Kind() == reflect.Float32 || b.Kind() == reflect.Float64
	switch {
	case !aFloat && !bFloat:
		return intValuesEqual(a, b)
	case aFloat && bFloat:
		return a.Float() == b.Float()
	case aFloat:
		return floatIntMathEqual(a.Float(), b)
	default:
		return floatIntMathEqual(b.Float(), a)
	}
}

// intValuesEqual 按数学值比较两个整型反射值，负数与无符号值永不相等。
func intValuesEqual(a, b reflect.Value) bool {
	aSigned := isSignedIntKind(a.Kind())
	bSigned := isSignedIntKind(b.Kind())
	switch {
	case aSigned && bSigned:
		return a.Int() == b.Int()
	case aSigned:
		if a.Int() < 0 {
			return false
		}
		return uint64(a.Int()) == b.Uint()
	case bSigned:
		if b.Int() < 0 {
			return false
		}
		return a.Uint() == uint64(b.Int())
	default:
		return a.Uint() == b.Uint()
	}
}

// floatIntMathEqual 判断浮点值与整型值是否数学相等。
// 仅接受可精确表示的有限整数值，超过精度域的浮点一律不等。
func floatIntMathEqual(f float64, i reflect.Value) bool {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return false
	}
	if isSignedIntKind(i.Kind()) {
		iv := i.Int()
		if f < 0 {
			if f < -9223372036854775808.0 { // < -2^63，超出 int64 精确域
				return false
			}
			return iv == int64(f)
		}
		if iv < 0 {
			return false
		}
		if f >= 9223372036854775808.0 { // >= 2^63，超出 int64 精确域
			return false
		}
		return iv == int64(f)
	}
	if f < 0 || f >= 18446744073709551616.0 { // 负数或 >= 2^64
		return false
	}
	return i.Uint() == uint64(f)
}

// EqualValuesf 断言 expected 与 actual 在类型转换后值相等，支持格式化消息。
func EqualValuesf(t TestingT, expected, actual any, format string, args ...any) bool {
	t.Helper()
	return EqualValues(t, expected, actual, append([]any{format}, args...)...)
}

func pointerOf(v any) (uintptr, bool) {
	if v == nil {
		return 0, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.Pointer(), true
	default:
		return 0, false
	}
}

// Same 断言 expected 与 actual 指向同一个内存地址。
func Same(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	expPtr, ok1 := pointerOf(expected)
	actPtr, ok2 := pointerOf(actual)
	if !ok1 || !ok2 || expPtr != actPtr {
		t.Errorf("Not same pointer:\nexpected: %p\nactual  : %p%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	return true
}

// NotSame 断言 expected 与 actual 不指向同一个内存地址。
func NotSame(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	t.Helper()
	expPtr, ok1 := pointerOf(expected)
	actPtr, ok2 := pointerOf(actual)
	if !ok1 || !ok2 {
		t.Errorf("NotSame requires pointer-like values, got %T and %T%s", expected, actual, formatMsg(msgAndArgs...))
		return false
	}
	if expPtr == actPtr {
		t.Errorf("Should not be same pointer: %p%s", actual, formatMsg(msgAndArgs...))
		return false
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
