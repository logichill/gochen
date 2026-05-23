package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryStore_SaveNilAndDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	require.NoError(t, store.Save(ctx, nil))
	require.NoError(t, store.SaveDefinition(ctx, nil))

	state := &State{ID: "wf-1", DefinitionID: "def-1"}
	require.NoError(t, store.Save(ctx, state))
	require.NoError(t, store.SaveDefinition(ctx, &Definition{
		ID:          "def-1",
		StartNodeID: "node-1",
		Nodes:       []Node{{ID: "node-1"}},
	}))

	gotState, err := store.Get(ctx, "wf-1")
	require.NoError(t, err)
	require.NotNil(t, gotState)
	require.Equal(t, ID("wf-1"), gotState.ID)

	gotDefinition, err := store.GetDefinition(ctx, "def-1")
	require.NoError(t, err)
	require.NotNil(t, gotDefinition)
	require.Equal(t, "def-1", gotDefinition.ID)

	require.NoError(t, store.Delete(ctx, "wf-1"))
	require.NoError(t, store.DeleteDefinition(ctx, "def-1"))

	gotState, err = store.Get(ctx, "wf-1")
	require.NoError(t, err)
	require.Nil(t, gotState)

	gotDefinition, err = store.GetDefinition(ctx, "def-1")
	require.NoError(t, err)
	require.Nil(t, gotDefinition)
}

func TestMemoryStore_ClonesDefinitionAndState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	require.NoError(t, store.SaveDefinition(ctx, &Definition{
		ID:          "def-2",
		StartNodeID: "node-1",
		Nodes: []Node{
			{ID: "node-1", Name: "original", Next: []string{"join"}},
			{ID: "join"},
		},
	}))
	require.NoError(t, store.Save(ctx, &State{
		ID:            "wf-2",
		DefinitionID:  "def-2",
		ActiveNodeIDs: []string{"node-1"},
		PendingJoins: []PendingJoin{
			{NodeID: "join", ExpectedCount: 2, ArrivedFrom: []string{"left"}},
		},
		Data: map[string]any{
			"nested": map[string]any{
				"value": "original",
			},
		},
		CompletedNodeIDs: []string{"node-1"},
		History: []HistoryEntry{
			{Action: "create", NodeID: "node-1"},
		},
	}))

	definition, err := store.GetDefinition(ctx, "def-2")
	require.NoError(t, err)
	definition.Nodes[0].Name = "changed"
	definition.Nodes[0].Next[0] = "changed"

	state, err := store.Get(ctx, "wf-2")
	require.NoError(t, err)
	state.Data["nested"].(map[string]any)["value"] = "changed"
	state.CompletedNodeIDs[0] = "mutated"
	state.ActiveNodeIDs[0] = "mutated"
	state.PendingJoins[0].ArrivedFrom[0] = "mutated"
	state.History[0].Action = "mutated"

	definition, err = store.GetDefinition(ctx, "def-2")
	require.NoError(t, err)
	require.Equal(t, "original", definition.Nodes[0].Name)
	require.Equal(t, "join", definition.Nodes[0].Next[0])

	state, err = store.Get(ctx, "wf-2")
	require.NoError(t, err)
	require.Equal(t, "original", state.Data["nested"].(map[string]any)["value"])
	require.Equal(t, []string{"node-1"}, state.CompletedNodeIDs)
	require.Equal(t, []string{"node-1"}, state.ActiveNodeIDs)
	require.Equal(t, []string{"left"}, state.PendingJoins[0].ArrivedFrom)
	require.Equal(t, "create", state.History[0].Action)
}

func TestMemoryStore_CloneStatePreservesNilMapSliceItems(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	require.NoError(t, store.Save(ctx, &State{
		ID:           "wf-nil-map-slice",
		DefinitionID: "def",
		Data: map[string]any{
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

	items[1]["name"] = "changed"
	state, err = store.Get(ctx, "wf-nil-map-slice")
	require.NoError(t, err)
	items = state.Data["items"].([]map[string]any)
	require.Equal(t, "original", items[1]["name"])
}
