package meta_test

import (
	"gochen/domain/meta"
	"testing"
)

func TestCloneSeparatesMutableStateAndPreservesPointerCycles(t *testing.T) {
	type node struct {
		Value      *string
		Next       *node
		Values     []int
		Properties map[string]*string
	}
	value := "original"
	input := &node{Value: &value, Values: []int{1}, Properties: map[string]*string{"value": &value}}
	input.Next = input
	copy, err := meta.Clone(input)
	if err != nil {
		t.Fatal(err)
	}
	if copy == input || copy.Next != copy || copy.Value != copy.Properties["value"] {
		t.Fatal("pointer graph was not preserved")
	}
	*copy.Value = "changed"
	copy.Values[0] = 2
	copy.Properties["new"] = copy.Value
	if *input.Value != "original" || input.Values[0] != 1 || len(input.Properties) != 1 {
		t.Fatal("clone changed original")
	}
}

func TestClonePreservesContainerCycles(t *testing.T) {
	input := map[string]any{}
	input["self"] = input
	values := make([]any, 1)
	values[0] = values
	input["values"] = values
	copy, err := meta.Clone(input)
	if err != nil {
		t.Fatal(err)
	}
	copy["self"].(map[string]any)["marker"] = true
	if copy["marker"] != true || input["marker"] != nil {
		t.Fatal("map cycle or isolation lost")
	}
	copiedValues := copy["values"].([]any)
	copiedValues[0].([]any)[0] = "changed"
	if copiedValues[0] != "changed" {
		t.Fatal("slice cycle lost")
	}
	if _, ok := values[0].([]any); !ok {
		t.Fatal("original slice mutated")
	}
}
