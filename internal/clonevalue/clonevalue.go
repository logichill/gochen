package clonevalue

import "reflect"

const maxCloneDepth = 128

// Clone copies the mutable value shapes used by framework message payloads and workflow State.Data.
func Clone(value any) any {
	return clone(value, 0)
}

func clone(value any, depth int) any {
	if depth >= maxCloneDepth {
		return value
	}
	switch typed := value.(type) {
	case map[string]any:
		return mapStringAny(typed, depth+1)
	case []any:
		return sliceAny(typed, depth+1)
	case []byte:
		if typed == nil {
			return []byte(nil)
		}
		out := make([]byte, len(typed))
		copy(out, typed)
		return out
	case []string:
		if typed == nil {
			return []string(nil)
		}
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []map[string]any:
		if typed == nil {
			return []map[string]any(nil)
		}
		out := make([]map[string]any, len(typed))
		for i := range typed {
			out[i] = mapStringAny(typed[i], depth+1)
		}
		return out
	case map[string]string:
		if typed == nil {
			return map[string]string(nil)
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case []map[string]string:
		if typed == nil {
			return []map[string]string(nil)
		}
		out := make([]map[string]string, len(typed))
		for i := range typed {
			if typed[i] == nil {
				continue
			}
			item := make(map[string]string, len(typed[i]))
			for k, v := range typed[i] {
				item[k] = v
			}
			out[i] = item
		}
		return out
	case []int:
		if typed == nil {
			return []int(nil)
		}
		out := make([]int, len(typed))
		copy(out, typed)
		return out
	default:
		return cloneReflect(value, depth+1)
	}
}

// cloneReflect 为快速路径未覆盖的可变容器（其它 slice/map/array 及指向它们的指针）
// 提供反射兜底深拷贝，避免 default 分支原样返回导致底层内存别名、破坏隔离语义。
// 标量、结构体等无法安全逐字段复制的类型按原值返回（与历史行为一致），不做有损拷贝。
func cloneReflect(value any, depth int) any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	out, cloned := cloneReflectValue(rv, depth)
	if !cloned {
		return value
	}
	return out.Interface()
}

func cloneReflectValue(rv reflect.Value, depth int) (reflect.Value, bool) {
	if depth >= maxCloneDepth {
		return rv, false
	}
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return rv, false
		}
		elem, cloned := cloneReflectValue(rv.Elem(), depth+1)
		if !cloned {
			return rv, false
		}
		out := reflect.New(rv.Elem().Type())
		out.Elem().Set(elem)
		return out, true
	case reflect.Slice:
		if rv.IsNil() {
			return rv, false
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if elem, cloned := cloneReflectValue(rv.Index(i), depth+1); cloned {
				out.Index(i).Set(elem)
			} else {
				out.Index(i).Set(rv.Index(i))
			}
		}
		return out, true
	case reflect.Map:
		if rv.IsNil() {
			return rv, false
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		for _, k := range rv.MapKeys() {
			v := rv.MapIndex(k)
			if elem, cloned := cloneReflectValue(v, depth+1); cloned {
				out.SetMapIndex(k, elem)
			} else {
				out.SetMapIndex(k, v)
			}
		}
		return out, true
	case reflect.Interface:
		if rv.IsNil() {
			return rv, false
		}
		elem, cloned := cloneReflectValue(rv.Elem(), depth+1)
		if !cloned {
			return rv, false
		}
		// 重新包装回原 interface 类型，保持静态类型不变。
		out := reflect.New(rv.Type()).Elem()
		out.Set(elem)
		return out, true
	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		any := false
		for i := 0; i < rv.Len(); i++ {
			if elem, cloned := cloneReflectValue(rv.Index(i), depth+1); cloned {
				out.Index(i).Set(elem)
				any = true
			} else {
				out.Index(i).Set(rv.Index(i))
			}
		}
		return out, any
	default:
		return rv, false
	}
}

// MapStringAny copies a map[string]any recursively for supported mutable child values.
func MapStringAny(source map[string]any) map[string]any {
	return mapStringAny(source, 0)
}

func mapStringAny(source map[string]any, depth int) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for k, v := range source {
		out[k] = clone(v, depth+1)
	}
	return out
}

// SliceAny copies a []any recursively for supported mutable child values.
func SliceAny(source []any) []any {
	return sliceAny(source, 0)
}

func sliceAny(source []any, depth int) []any {
	if source == nil {
		return nil
	}
	out := make([]any, len(source))
	for i := range source {
		out[i] = clone(source[i], depth+1)
	}
	return out
}
