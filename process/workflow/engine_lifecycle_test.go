package workflow

import (
	"context"

	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"
)

func TestEngine_SaveDefinitionPreservesCreatedAtOnUpdate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manualClock := clock.NewManualClock(time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-timestamps",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	}))
	created, err := store.GetDefinition(ctx, "definition-timestamps", 0)
	require.NoError(t, err)
	require.Equal(t, manualClock.Now(), created.CreatedAt)
	require.Equal(t, manualClock.Now(), created.UpdatedAt)

	manualClock.Advance(time.Hour)
	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-timestamps",
		Name:        "updated",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start", Name: "renamed"}},
	}))

	updated, err := store.GetDefinition(ctx, "definition-timestamps", 0)
	require.NoError(t, err)
	require.Equal(t, created.CreatedAt, updated.CreatedAt)
	require.Equal(t, manualClock.Now(), updated.UpdatedAt)
	require.True(t, updated.UpdatedAt.After(updated.CreatedAt))
	require.Equal(t, "updated", updated.Name)
	require.Equal(t, "renamed", updated.Nodes[0].Name)
}

func TestEngine_SaveDefinitionSkipsLookupWhenCreatedAtProvided(t *testing.T) {
	ctx := context.Background()
	store := &saveOnlyDefinitionStore{}
	manualClock := clock.NewManualClock(time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)
	createdAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "definition-with-created-at",
		Version:     1,
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
		CreatedAt:   createdAt,
	}))

	require.Zero(t, store.getDefinitionCalls)
	require.NotNil(t, store.saved)
	require.Equal(t, createdAt, store.saved.CreatedAt)
	require.Equal(t, manualClock.Now(), store.saved.UpdatedAt)
}

func TestEngine_SaveDefinitionSerializesConcurrentCreateForSameID(t *testing.T) {
	ctx := context.Background()
	store := newBlockingMissingDefinitionStore()
	manualClock := clock.NewManualClock(time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC))
	engine := NewEngine(store).WithClock(manualClock)

	errs := make(chan error, 2)
	go func() {
		errs <- engine.SaveDefinition(ctx, &Definition{
			ID:          "definition-concurrent-create",
			Name:        "first",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start"}},
		})
	}()

	<-store.firstMissingGetStarted

	go func() {
		errs <- engine.SaveDefinition(ctx, &Definition{
			ID:          "definition-concurrent-create",
			Name:        "second",
			StartNodeID: "start",
			Nodes:       []Node{{ID: "start", Name: "renamed"}},
		})
	}()

	select {
	case <-store.secondMissingGetStarted:
		t.Fatal("expected same definition saves to serialize before second GetDefinition")
	case <-time.After(50 * time.Millisecond):
	}

	manualClock.Advance(time.Hour)
	close(store.releaseFirstMissingGet)

	for i := 0; i < 2; i++ {
		require.NoError(t, <-errs)
	}

	saved, err := store.GetDefinition(ctx, "definition-concurrent-create", 0)
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC), saved.CreatedAt)
	require.Equal(t, manualClock.Now(), saved.UpdatedAt)
	require.Equal(t, "second", saved.Name)
	require.Equal(t, "renamed", saved.Nodes[0].Name)
}

func TestEngine_StartInstanceSerializesConcurrentCalls(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "start-concurrent",
		StartNodeID: "only",
		Nodes:       []Node{{ID: "only"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-start-concurrent"), "start-concurrent"))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.StartInstance(ctx, ID("wf-start-concurrent"))
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(err, errors.Conflict))
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 7, conflicts)
}

func TestEngine_StartInstanceAcrossEnginesUsesStoreVersioning(t *testing.T) {
	ctx := context.Background()
	store := newBlockingVersionStore()
	engineA := NewEngine(store)
	engineB := NewEngine(store)

	require.NoError(t, engineA.SaveDefinition(ctx, &Definition{
		ID:          "cross-engine",
		StartNodeID: "only",
		Nodes:       []Node{{ID: "only"}},
	}))
	require.NoError(t, engineA.CreateInstance(ctx, ID("wf-cross-engine"), "cross-engine"))

	started := make(chan struct{}, 2)
	done := make(chan error, 2)
	go func() {
		started <- struct{}{}
		done <- engineA.StartInstance(ctx, ID("wf-cross-engine"))
	}()
	go func() {
		started <- struct{}{}
		done <- engineB.StartInstance(ctx, ID("wf-cross-engine"))
	}()

	<-started
	<-started
	<-store.ready
	<-store.ready
	close(store.barrier)

	successes := 0
	conflicts := 0
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			successes++
			continue
		}
		require.Truef(t, errors.Is(err, errors.Conflict), "expected conflict, got %v", err)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	state, err := store.Get(ctx, ID("wf-cross-engine"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, state.Status)
	require.Equal(t, uint64(2), state.Version)
}

func TestEngine_CreateInstancePersistsInitialDataAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)
	initial := map[string]any{
		"request_id": "REQ-1",
		"items":      []map[string]any{{"sku": "SKU-1"}},
	}

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "with-data",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}))
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("wf-with-data"), "with-data", initial))
	initial["request_id"] = "mutated"
	initial["items"].([]map[string]any)[0]["sku"] = "mutated"

	state, err := store.Get(ctx, ID("wf-with-data"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Version)
	require.Equal(t, "REQ-1", state.Data["request_id"])
	require.Equal(t, "SKU-1", state.Data["items"].([]map[string]any)[0]["sku"])
}

func TestEngine_CreateInstanceSerializesConcurrentDuplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "create-concurrent",
		StartNodeID: "node",
		Nodes:       []Node{{ID: "node"}},
	}))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.CreateInstance(ctx, ID("wf-create-concurrent"), "create-concurrent")
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(err, errors.Conflict))
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

func TestEngine_StartInstanceWithMutationPersistsMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	require.NoError(t, engine.SaveDefinition(ctx, &Definition{
		ID:          "start-with-mutation",
		StartNodeID: "start",
		Nodes:       []Node{{ID: "start"}},
	}))
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-start-mutation"), "start-with-mutation"))

	require.NoError(t, engine.StartInstanceWithMutation(ctx, ID("wf-start-mutation"), func(st *State, now time.Time) error {
		st.Data["tag"] = "started"
		return nil
	}))

	state, err := store.Get(ctx, ID("wf-start-mutation"))
	require.NoError(t, err)
	require.Equal(t, "started", state.Data["tag"])
}

func TestEngine_LifecycleSuspendResumeTerminate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	def := &Definition{
		ID:          "lifecycle-def",
		StartNodeID: "step1",
		Nodes: []Node{
			{ID: "step1", Edges: NewEdges("step2")},
			{ID: "step2"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))

	// 1. 测试 Suspend 和 Resume
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-life-1"), "lifecycle-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-life-1")))

	// 挂起
	require.NoError(t, engine.SuspendInstance(ctx, ID("wf-life-1"), "waiting for manual audit"))
	st, err := store.Get(ctx, ID("wf-life-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusSuspended, st.Status)

	// 挂起状态下禁止推进
	err = engine.AdvanceNode(ctx, ID("wf-life-1"), "step1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))

	// 恢复
	require.NoError(t, engine.ResumeInstance(ctx, ID("wf-life-1")))
	st, err = store.Get(ctx, ID("wf-life-1"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusRunning, st.Status)

	// 恢复后可以正常推进
	require.NoError(t, engine.AdvanceNode(ctx, ID("wf-life-1"), "step1"))

	// 2. 测试 Terminate
	require.NoError(t, engine.CreateInstance(ctx, ID("wf-life-2"), "lifecycle-def"))
	require.NoError(t, engine.StartInstance(ctx, ID("wf-life-2")))

	require.NoError(t, engine.TerminateInstance(ctx, ID("wf-life-2"), "cancelled by user"))
	st2, err := store.Get(ctx, ID("wf-life-2"))
	require.NoError(t, err)
	require.Equal(t, InstanceStatusTerminated, st2.Status)
	require.Empty(t, st2.ActiveNodeIDs)

	// 终止状态不可再推进
	err = engine.AdvanceNode(ctx, ID("wf-life-2"), "step1")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
}
