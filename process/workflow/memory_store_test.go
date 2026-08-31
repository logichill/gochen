package workflow

import (
	"context"
	"testing"

	"gochen/testkit/require"
)

func TestMemoryStore_SaveNilAndDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	require.NoError(t, store.Save(ctx, nil))
	require.NoError(t, store.SaveDefinition(ctx, nil))

	state := &State{ID: "wf-1", DefinitionID: "def-1", DefinitionVersion: 1}
	require.NoError(t, store.Save(ctx, state))
	require.NoError(t, store.SaveDefinition(ctx, &Definition{
		ID:          "def-1",
		Version:     1,
		StartNodeID: "node-1",
		Nodes:       []Node{{ID: "node-1"}},
	}))

	gotState, err := store.Get(ctx, "wf-1")
	require.NoError(t, err)
	require.NotNil(t, gotState)
	require.Equal(t, ID("wf-1"), gotState.ID)

	gotDefinition, err := store.GetDefinition(ctx, "def-1", 1)
	require.NoError(t, err)
	require.NotNil(t, gotDefinition)
	require.Equal(t, "def-1", gotDefinition.ID)
	require.Equal(t, uint32(1), gotDefinition.Version)

	require.NoError(t, store.Delete(ctx, "wf-1"))
	require.NoError(t, store.DeleteDefinition(ctx, "def-1", 1))

	gotState, err = store.Get(ctx, "wf-1")
	require.NoError(t, err)
	require.Nil(t, gotState)

	gotDefinition, err = store.GetDefinition(ctx, "def-1", 1)
	require.NoError(t, err)
	require.Nil(t, gotDefinition)
}

func TestMemoryStore_Versioning(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// 保存 v1
	def1 := &Definition{
		ID:          "def-multi",
		Version:     1,
		StartNodeID: "v1-start",
		Nodes:       []Node{{ID: "v1-start"}},
	}
	require.NoError(t, store.SaveDefinition(ctx, def1))

	// 保存 v2
	def2 := &Definition{
		ID:          "def-multi",
		Version:     2,
		StartNodeID: "v2-start",
		Nodes:       []Node{{ID: "v2-start"}},
	}
	require.NoError(t, store.SaveDefinition(ctx, def2))

	// 查询最新版 (version=0)
	latest, err := store.GetDefinition(ctx, "def-multi", 0)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, uint32(2), latest.Version)
	require.Equal(t, "v2-start", latest.StartNodeID)

	// 查询特定 v1
	v1, err := store.GetDefinition(ctx, "def-multi", 1)
	require.NoError(t, err)
	require.NotNil(t, v1)
	require.Equal(t, uint32(1), v1.Version)
	require.Equal(t, "v1-start", v1.StartNodeID)

	// 删除全部版本
	require.NoError(t, store.DeleteDefinition(ctx, "def-multi", 0))
	deleted, err := store.GetDefinition(ctx, "def-multi", 0)
	require.NoError(t, err)
	require.Nil(t, deleted)
}

func TestMemoryStore_ClonesDefinitionAndState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	require.NoError(t, store.SaveDefinition(ctx, &Definition{
		ID:          "def-2",
		Version:     1,
		StartNodeID: "node-1",
		Nodes: []Node{
			{ID: "node-1", Name: "original", Edges: NewEdges("join")},
			{ID: "join"},
		},
	}))
	require.NoError(t, store.Save(ctx, &State{
		ID:                "wf-2",
		DefinitionID:      "def-2",
		DefinitionVersion: 1,
		ActiveNodeIDs:     []string{"node-1"},
		PendingJoins: []PendingJoin{
			{NodeID: "join", ExpectedCount: 2, ArrivedFrom: []string{"left"}},
		},
		Data: map[string]any{
			"nested": map[string]any{
				"value": "original",
			},
			"labels": map[string]string{
				"env": "prod",
			},
			"numbers": []int{1, 2, 3},
			"typed_items": []map[string]string{
				{"name": "original"},
			},
			"nil": nil,
		},
		CompletedNodeIDs: []string{"node-1"},
		History: []HistoryEntry{
			{Action: "create", NodeID: "node-1"},
		},
	}))

	definition, err := store.GetDefinition(ctx, "def-2", 1)
	require.NoError(t, err)
	definition.Nodes[0].Name = "changed"
	definition.Nodes[0].Edges[0].Target = "changed"

	state, err := store.Get(ctx, "wf-2")
	require.NoError(t, err)
	state.Data["nested"].(map[string]any)["value"] = "changed"
	state.Data["labels"].(map[string]string)["env"] = "dev"
	state.Data["numbers"].([]int)[0] = 99
	state.Data["typed_items"].([]map[string]string)[0]["name"] = "changed"
	state.CompletedNodeIDs[0] = "mutated"
	state.ActiveNodeIDs[0] = "mutated"
	state.PendingJoins[0].ArrivedFrom[0] = "mutated"
	state.History[0].Action = "mutated"

	definition, err = store.GetDefinition(ctx, "def-2", 1)
	require.NoError(t, err)
	require.Equal(t, "original", definition.Nodes[0].Name)
	require.Equal(t, "join", definition.Nodes[0].Edges[0].Target)

	state, err = store.Get(ctx, "wf-2")
	require.NoError(t, err)
	require.Equal(t, "original", state.Data["nested"].(map[string]any)["value"])
	require.Equal(t, "prod", state.Data["labels"].(map[string]string)["env"])
	require.Equal(t, []int{1, 2, 3}, state.Data["numbers"])
	require.Equal(t, "original", state.Data["typed_items"].([]map[string]string)[0]["name"])
	require.Nil(t, state.Data["nil"])
	require.Equal(t, []string{"node-1"}, state.CompletedNodeIDs)
	require.Equal(t, []string{"node-1"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"left"}, state.PendingJoins[0].ArrivedFrom)
	require.Equal(t, "create", state.History[0].Action)
}

func TestMemoryStore_CloneStatePreservesNilMapSliceItems(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	var nilAnySlice []any
	var nilByteSlice []byte
	var nilMapSlice []map[string]any
	require.NoError(t, store.Save(ctx, &State{
		ID:           "wf-nil-map-slice",
		DefinitionID: "def",
		Data: map[string]any{
			"any_slice": nilAnySlice,
			"bytes":     nilByteSlice,
			"map_slice": nilMapSlice,
			"items": []map[string]any{
				nil,
				{"name": "original"},
			},
		},
	}))

	state, err := store.Get(ctx, "wf-nil-map-slice")
	require.NoError(t, err)
	items := state.Data["items"].([]map[string]any)
	require.Len(t, items, 2)
	require.Nil(t, items[0])
	require.Equal(t, "original", items[1]["name"])
	require.Nil(t, state.Data["any_slice"].([]any))
	require.Nil(t, state.Data["bytes"].([]byte))
	require.Nil(t, state.Data["map_slice"].([]map[string]any))

	items[1]["name"] = "changed"
	state, err = store.Get(ctx, "wf-nil-map-slice")
	require.NoError(t, err)
	items = state.Data["items"].([]map[string]any)
	require.Equal(t, "original", items[1]["name"])
}

func TestMemoryStore_ClonesOverlappingSlicesByLength(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	values := []int{1, 2, 3}

	require.NoError(t, store.Save(ctx, &State{
		ID:           "wf-overlapping-slices",
		DefinitionID: "def",
		Data: map[string]any{
			"short": values[:1],
			"long":  values[:2],
		},
	}))

	state, err := store.Get(ctx, "wf-overlapping-slices")
	require.NoError(t, err)
	require.Equal(t, []int{1}, state.Data["short"])
	require.Equal(t, []int{1, 2}, state.Data["long"])
}

func TestMemoryStore_CloneDataKeepsUnsupportedReferenceValuesStable(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	ch := make(chan string)
	fn := func() string { return "ok" }

	require.NoError(t, store.Save(ctx, &State{
		ID:           "wf-reference-values",
		DefinitionID: "def",
		Data: map[string]any{
			"channel": ch,
			"func":    fn,
		},
	}))

	state, err := store.Get(ctx, "wf-reference-values")
	require.NoError(t, err)
	require.Equal(t, ch, state.Data["channel"])
	require.NotNil(t, state.Data["func"])
	require.Equal(t, "ok", state.Data["func"].(func() string)())
}
