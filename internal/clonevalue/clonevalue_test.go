package clonevalue

import "testing"

// TestClone_ReflectFallbackIsolatesUnlistedSlice 验证快速路径未覆盖的可变类型
// （如 []int64）经反射兜底后不再与原值共享底层数组。
func TestClone_ReflectFallbackIsolatesUnlistedSlice(t *testing.T) {
	src := []int64{1, 2, 3}
	cloned, ok := Clone(src).([]int64)
	if !ok {
		t.Fatalf("expected []int64 clone, got %T", Clone(src))
	}
	cloned[0] = 99
	if src[0] != 1 {
		t.Fatalf("clone aliased original slice: src[0]=%d", src[0])
	}
}

// TestClone_ReflectFallbackIsolatesNestedMap 验证嵌套于未覆盖容器中的 map 被深拷贝。
func TestClone_ReflectFallbackIsolatesNestedMap(t *testing.T) {
	src := map[string]map[string]int{"a": {"x": 1}}
	cloned, ok := Clone(src).(map[string]map[string]int)
	if !ok {
		t.Fatalf("expected nested map clone, got %T", Clone(src))
	}
	cloned["a"]["x"] = 99
	if src["a"]["x"] != 1 {
		t.Fatalf("clone aliased nested map: src[a][x]=%d", src["a"]["x"])
	}
}

// TestClone_NilAndScalarPassThrough 验证 nil 与标量按原值返回。
func TestClone_NilAndScalarPassThrough(t *testing.T) {
	if got := Clone(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	if got := Clone(42); got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
}

// TestClone_ReflectFallbackIsolatesInterfaceWrappedContainer 验证当未覆盖容器的
// 元素是 interface（[]any 元素）且包裹的是快速路径无法识别的可变容器时，
// 反射兜底会穿过 interface 做深拷贝，而非按引用别名。
func TestClone_ReflectFallbackIsolatesInterfaceWrappedContainer(t *testing.T) {
	// [][]any 不在快速路径覆盖中，触发反射 Slice 分支；
	// 其元素 []any 又以 reflect.Interface 形态出现，需要被解包深拷贝。
	src := [][]any{{[]int{1, 2}}}
	cloned, ok := Clone(src).([][]any)
	if !ok {
		t.Fatalf("expected [][]any clone, got %T", Clone(src))
	}
	inner := cloned[0][0].([]int)
	inner[0] = 99
	if src[0][0].([]int)[0] != 1 {
		t.Fatalf("clone aliased interface-wrapped slice: src=%v", src[0][0])
	}
}

func TestClone_DepthLimitReturnsStableValueForVeryDeepContainers(t *testing.T) {
	leaf := []int{1}
	var value any = leaf
	for i := 0; i < maxCloneDepth+10; i++ {
		value = []any{value}
	}

	cloned := Clone(value)
	if cloned == nil {
		t.Fatalf("expected stable clone value")
	}
}
