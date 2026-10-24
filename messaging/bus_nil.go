package messaging

import "reflect"

// isNilValue reports whether v is nil (including typed-nil pointers, funcs,
// channels, maps, slices and interfaces). Used to reject partially-constructed
// middleware/handlers at registration time instead of panicking in the hot path.
func isNilValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
