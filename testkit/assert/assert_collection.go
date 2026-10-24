package assert

import (
	"encoding/json"
	"reflect"
	"strings"
)

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
	return Len(t, object, length, append([]any{format}, args...)...)
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
	return Contains(t, s, contains, append([]any{format}, args...)...)
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
	return NotContains(t, s, contains, append([]any{format}, args...)...)
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
