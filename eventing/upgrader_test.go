package eventing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloneMap_PerformsDeepCopyForNestedStructures(t *testing.T) {
	original := map[string]any{
		"number": 42,
		"nested": map[string]any{
			"key": "value",
		},
		"list": []any{
			map[string]any{"item": 1},
			2,
		},
	}

	cloned := cloneMap(original)

	// 修改顶层与嵌套结构，确保不会影响原始数据
	cloned["number"] = 100

	nested, ok := cloned["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map in cloned payload")
	}
	nested["key"] = "changed"

	list, ok := cloned["list"].([]any)
	if !ok {
		t.Fatalf("expected list in cloned payload")
	}
	nestedInList, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map in list element")
	}
	nestedInList["item"] = 99

	// 原始数据不应受影响
	assert.Equal(t, 42, original["number"])

	origNested, ok := original["nested"].(map[string]any)
	if assert.True(t, ok) {
		assert.Equal(t, "value", origNested["key"])
	}

	origList, ok := original["list"].([]any)
	if assert.True(t, ok) {
		first, ok := origList[0].(map[string]any)
		if assert.True(t, ok) {
			assert.Equal(t, 1, first["item"])
		}
	}
}
