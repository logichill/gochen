package workflow

import (
	"context"

	"testing"
	"time"

	"gochen/testkit/require"
)

func TestEngine_SaveDefinitionAllowsDifferentIDsWhileOneLookupBlocks(t *testing.T) {
	ctx := context.Background()
	store := newBlockingMissingDefinitionStore()
	engine := NewEngine(store)

	firstErr := make(chan error, 1)
	go func() {
		firstErr <- engine.SaveDefinition(ctx, &Definition{
			ID:          "blocked-definition",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	<-store.firstMissingGetStarted

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- engine.SaveDefinition(ctx, &Definition{
			ID:          "independent-definition",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	select {
	case err := <-secondErr:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected different definition saves not to wait on blocked lookup")
	}

	close(store.releaseFirstMissingGet)
	require.NoError(t, <-firstErr)
}

func TestEngine_SaveDefinition_IgnoresNotFoundLookup(t *testing.T) {
	ctx := context.Background()
	store := notFoundDefinitionStore{MemoryStore: NewMemoryStore()}
	engine := NewEngine(store)

	err := engine.SaveDefinition(ctx, &Definition{
		ID:          "new-definition",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	})
	require.NoError(t, err)

	saved, getErr := store.MemoryStore.GetDefinition(ctx, "new-definition", 0)
	require.NoError(t, getErr)
	require.NotNil(t, saved)
}

func TestEngine_LockDefinitionCleansUpReferenceCount(t *testing.T) {
	engine := &Engine{}

	unlock := engine.lockDefinition("def-1")
	engine.definitionMu.Lock()
	require.Contains(t, engine.definitionLocks, "def-1")
	require.Equal(t, 1, engine.definitionLocks["def-1"].refs)
	engine.definitionMu.Unlock()

	unlock()

	engine.definitionMu.Lock()
	require.NotContains(t, engine.definitionLocks, "def-1")
	engine.definitionMu.Unlock()
}

func TestEngine_DefinitionVersioningIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	// 发布 v1 定义：start -> step_v1 -> end
	defV1 := &Definition{
		ID:          "multi-ver",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("step_v1")},
			{ID: "step_v1", Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, defV1))
	require.Equal(t, uint32(1), defV1.Version)

	// 创建实例 1，绑定 v1
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-v1"), "multi-ver"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-v1")))

	// 发布 v2 定义：直接 start -> step_v2 -> end（移除了 step_v1）
	defV2 := &Definition{
		ID:          "multi-ver",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("step_v2")},
			{ID: "step_v2", Edges: NewEdges("end")},
			{ID: "end"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, defV2))
	require.Equal(t, uint32(2), defV2.Version)

	// 创建实例 2，默认绑定最新版 v2
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-v2"), "multi-ver"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-v2")))

	// 实例 1 推进 step_v1：由于多版本隔离，实例 1 仍然绑定 v1 定义，能正常推进至 step_v1
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-v1"), "start"))
	st1, err := store.Get(ctx, ID("wf-v1"))
	require.NoError(t, err)
	require.Equal(t, uint32(1), st1.DefinitionVersion)
	require.Equal(t, []string{"step_v1"}, st1.ActiveNodeIDs)

	// 实例 2 推进 start：走向 v2 的 step_v2
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-v2"), "start"))
	st2, err := store.Get(ctx, ID("wf-v2"))
	require.NoError(t, err)
	require.Equal(t, uint32(2), st2.DefinitionVersion)
	require.Equal(t, []string{"step_v2"}, st2.ActiveNodeIDs)
}
