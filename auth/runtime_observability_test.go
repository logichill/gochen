package auth

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gochen/observe"
)

type blockingSnapshotStore struct {
	*MemoryPolicySnapshotStore
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	loadCalls   atomic.Int32
}

func newBlockingSnapshotStore() *blockingSnapshotStore {
	return &blockingSnapshotStore{
		MemoryPolicySnapshotStore: NewMemoryPolicySnapshotStore(),
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
}

func (s *blockingSnapshotStore) LoadPolicySnapshot(ctx context.Context, key string) (*PolicySnapshot, error) {
	s.loadCalls.Add(1)
	s.startedOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.MemoryPolicySnapshotStore.LoadPolicySnapshot(ctx, key)
}

func TestStoreBackedSnapshotResolver_BoundedStalenessUsesCache(t *testing.T) {
	store := NewMemoryPolicySnapshotStore()
	require.NoError(t, store.SavePolicySnapshot(context.Background(), PolicySnapshot{
		Key:       "default",
		Version:   "snap-v1",
		Timestamp: time.Now(),
	}))
	resolver, err := NewStoreBackedSnapshotResolver(store, StaticPolicySnapshotKeyResolver("default"), time.Hour)
	require.NoError(t, err)

	ctx := context.Background()
	ctx, err = WithPrincipal(ctx, Principal{SubjectID: 7})
	require.NoError(t, err)
	ctx, err = WithConsistencyMode(ctx, ConsistencyModeBoundedStaleness)
	require.NoError(t, err)
	ctx, err = WithSnapshotResolver(ctx, resolver)
	require.NoError(t, err)

	_, eval, err := BindAuthzEvalContext(ctx)
	require.NoError(t, err)
	require.Equal(t, "snap-v1", eval.SnapshotVersion)
	require.Equal(t, "default", eval.SnapshotKey)
	require.False(t, eval.SnapshotCacheHit)

	require.NoError(t, store.SavePolicySnapshot(context.Background(), PolicySnapshot{
		Key:       "default",
		Version:   "snap-v2",
		Timestamp: time.Now().Add(time.Second),
	}))

	ctx2 := context.Background()
	ctx2, err = WithPrincipal(ctx2, Principal{SubjectID: 7})
	require.NoError(t, err)
	ctx2, err = WithConsistencyMode(ctx2, ConsistencyModeBoundedStaleness)
	require.NoError(t, err)
	ctx2, err = WithSnapshotResolver(ctx2, resolver)
	require.NoError(t, err)

	_, cachedEval, err := BindAuthzEvalContext(ctx2)
	require.NoError(t, err)
	require.Equal(t, "snap-v1", cachedEval.SnapshotVersion)
	require.True(t, cachedEval.SnapshotCacheHit)

	ctx3 := context.Background()
	ctx3, err = WithPrincipal(ctx3, Principal{SubjectID: 7})
	require.NoError(t, err)
	ctx3, err = WithConsistencyMode(ctx3, ConsistencyModeStrong)
	require.NoError(t, err)
	ctx3, err = WithSnapshotResolver(ctx3, resolver)
	require.NoError(t, err)

	_, strongEval, err := BindAuthzEvalContext(ctx3)
	require.NoError(t, err)
	require.Equal(t, "snap-v2", strongEval.SnapshotVersion)
	require.False(t, strongEval.SnapshotCacheHit)
}

func TestMemoryPolicySnapshotStore_DefensivelyCopiesMetadata(t *testing.T) {
	store := NewMemoryPolicySnapshotStore()
	metadata := map[string]any{
		"source": "seed",
		"nested": map[string]any{"scope": "tenant-a"},
		"rules":  []any{"rule-a", map[string]any{"kind": "doc"}},
		"tags":   []string{"alpha"},
		"bytes":  []byte("abc"),
	}
	require.NoError(t, store.SavePolicySnapshot(context.Background(), PolicySnapshot{
		Key:       "default",
		Version:   "snap-v1",
		Timestamp: time.Now(),
		Metadata:  metadata,
	}))
	metadata["source"] = "mutated-before-load"
	metadata["nested"].(map[string]any)["scope"] = "mutated-before-load"
	metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-before-load"
	metadata["tags"].([]string)[0] = "mutated-before-load"
	metadata["bytes"].([]byte)[0] = 'x'

	loaded, err := store.LoadPolicySnapshot(context.Background(), "default")
	require.NoError(t, err)
	require.Equal(t, "seed", loaded.Metadata["source"])
	require.Equal(t, "tenant-a", loaded.Metadata["nested"].(map[string]any)["scope"])
	require.Equal(t, "doc", loaded.Metadata["rules"].([]any)[1].(map[string]any)["kind"])
	require.Equal(t, []string{"alpha"}, loaded.Metadata["tags"])
	require.Equal(t, []byte("abc"), loaded.Metadata["bytes"])
	loaded.Metadata["source"] = "mutated-after-load"
	loaded.Metadata["nested"].(map[string]any)["scope"] = "mutated-after-load"
	loaded.Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-after-load"
	loaded.Metadata["tags"].([]string)[0] = "mutated-after-load"
	loaded.Metadata["bytes"].([]byte)[0] = 'y'

	loadedAgain, err := store.LoadPolicySnapshot(context.Background(), "default")
	require.NoError(t, err)
	require.Equal(t, "seed", loadedAgain.Metadata["source"])
	require.Equal(t, "tenant-a", loadedAgain.Metadata["nested"].(map[string]any)["scope"])
	require.Equal(t, "doc", loadedAgain.Metadata["rules"].([]any)[1].(map[string]any)["kind"])
	require.Equal(t, []string{"alpha"}, loadedAgain.Metadata["tags"])
	require.Equal(t, []byte("abc"), loadedAgain.Metadata["bytes"])

	list, err := store.ListPolicySnapshots(context.Background(), "", 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list[0].Metadata["source"] = "mutated-after-list"
	list[0].Metadata["nested"].(map[string]any)["scope"] = "mutated-after-list"
	list[0].Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-after-list"
	list[0].Metadata["tags"].([]string)[0] = "mutated-after-list"
	list[0].Metadata["bytes"].([]byte)[0] = 'z'

	loadedAfterList, err := store.LoadPolicySnapshot(context.Background(), "default")
	require.NoError(t, err)
	require.Equal(t, "seed", loadedAfterList.Metadata["source"])
	require.Equal(t, "tenant-a", loadedAfterList.Metadata["nested"].(map[string]any)["scope"])
	require.Equal(t, "doc", loadedAfterList.Metadata["rules"].([]any)[1].(map[string]any)["kind"])
	require.Equal(t, []string{"alpha"}, loadedAfterList.Metadata["tags"])
	require.Equal(t, []byte("abc"), loadedAfterList.Metadata["bytes"])
}

func TestStoreBackedSnapshotResolver_SingleflightDoesNotInheritLeaderDeadline(t *testing.T) {
	store := newBlockingSnapshotStore()
	require.NoError(t, store.SavePolicySnapshot(context.Background(), PolicySnapshot{
		Key:       "default",
		Version:   "snap-v1",
		Timestamp: time.Now(),
	}))
	resolver, err := NewStoreBackedSnapshotResolver(store, StaticPolicySnapshotKeyResolver("default"), 0)
	require.NoError(t, err)
	resolver.SetLoadTimeout(500 * time.Millisecond)

	leaderCtx, leaderCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer leaderCancel()
	waiterCtx, waiterCancel := context.WithTimeout(context.Background(), time.Second)
	defer waiterCancel()

	leaderErrCh := make(chan error, 1)
	waiterCh := make(chan struct {
		res SnapshotResolution
		err error
	}, 1)

	go func() {
		_, err := resolver.ResolveSnapshot(leaderCtx, AuthzEvalContext{})
		leaderErrCh <- err
	}()

	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for snapshot load to start")
	}

	go func() {
		res, err := resolver.ResolveSnapshot(waiterCtx, AuthzEvalContext{})
		waiterCh <- struct {
			res SnapshotResolution
			err error
		}{res: res, err: err}
	}()

	select {
	case err := <-leaderErrCh:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for leader to exit on deadline")
	}

	close(store.release)

	select {
	case result := <-waiterCh:
		require.NoError(t, result.err)
		require.Equal(t, "snap-v1", result.res.Version)
		require.Equal(t, "default", result.res.Key)
		require.False(t, result.res.CacheHit)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for waiter to receive shared snapshot")
	}

	require.Equal(t, int32(1), store.loadCalls.Load())
}

func TestMemoryAuthzLogStore_DefensivelyCopiesEntries(t *testing.T) {
	store := NewMemoryAuthzLogStore()
	entry := AuthzLogEntry{
		Type:         AuthzLogEntryTypeDecision,
		DecisionID:   "decision-1",
		MatchedRules: []string{"rule-1"},
		Resources:    []AuthzLoggedResource{{Kind: "doc", ID: "doc-1"}},
		Metadata: map[string]any{
			"source": "seed",
			"nested": map[string]any{"scope": "tenant-a"},
			"rules":  []any{"rule-a", map[string]any{"kind": "doc"}},
			"tags":   []string{"alpha"},
			"bytes":  []byte("abc"),
		},
	}
	require.NoError(t, store.SaveAuthzLogEntry(context.Background(), entry))
	entry.MatchedRules[0] = "mutated-rule"
	entry.Resources[0].ID = "mutated-doc"
	entry.Metadata["source"] = "mutated-source"
	entry.Metadata["nested"].(map[string]any)["scope"] = "mutated-before-list"
	entry.Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-before-list"
	entry.Metadata["tags"].([]string)[0] = "mutated-before-list"
	entry.Metadata["bytes"].([]byte)[0] = 'x'

	entries, err := store.ListAuthzLogEntries(context.Background(), AuthzLogEntryTypeDecision, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, []string{"rule-1"}, entries[0].MatchedRules)
	require.Equal(t, "doc-1", entries[0].Resources[0].ID)
	require.Equal(t, "seed", entries[0].Metadata["source"])
	require.Equal(t, "tenant-a", entries[0].Metadata["nested"].(map[string]any)["scope"])
	require.Equal(t, "doc", entries[0].Metadata["rules"].([]any)[1].(map[string]any)["kind"])
	require.Equal(t, []string{"alpha"}, entries[0].Metadata["tags"])
	require.Equal(t, []byte("abc"), entries[0].Metadata["bytes"])

	entries[0].MatchedRules[0] = "mutated-after-list"
	entries[0].Resources[0].ID = "mutated-after-list"
	entries[0].Metadata["source"] = "mutated-after-list"
	entries[0].Metadata["nested"].(map[string]any)["scope"] = "mutated-after-list"
	entries[0].Metadata["rules"].([]any)[1].(map[string]any)["kind"] = "mutated-after-list"
	entries[0].Metadata["tags"].([]string)[0] = "mutated-after-list"
	entries[0].Metadata["bytes"].([]byte)[0] = 'y'

	entriesAgain, err := store.ListAuthzLogEntries(context.Background(), AuthzLogEntryTypeDecision, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"rule-1"}, entriesAgain[0].MatchedRules)
	require.Equal(t, "doc-1", entriesAgain[0].Resources[0].ID)
	require.Equal(t, "seed", entriesAgain[0].Metadata["source"])
	require.Equal(t, "tenant-a", entriesAgain[0].Metadata["nested"].(map[string]any)["scope"])
	require.Equal(t, "doc", entriesAgain[0].Metadata["rules"].([]any)[1].(map[string]any)["kind"])
	require.Equal(t, []string{"alpha"}, entriesAgain[0].Metadata["tags"])
	require.Equal(t, []byte("abc"), entriesAgain[0].Metadata["bytes"])
}

func TestAuthorizer_RecordsDecisionLogAndMetrics(t *testing.T) {
	snapshotStore := NewMemoryPolicySnapshotStore()
	require.NoError(t, snapshotStore.SavePolicySnapshot(context.Background(), PolicySnapshot{
		Key:       "default",
		Version:   "snap-v1",
		Timestamp: time.Now(),
	}))
	resolver, err := NewStoreBackedSnapshotResolver(snapshotStore, StaticPolicySnapshotKeyResolver("default"), time.Hour)
	require.NoError(t, err)

	logStore := NewMemoryAuthzLogStore()
	metrics := observe.NewInMemoryMetrics()
	authorizer, err := NewAuthorizer(EvaluatorFunc(func(ctx context.Context, principal Principal, permission string, resources []Resource) (AuthzDecision, error) {
		return AllowDecision(resources...), nil
	}), TypedResourceResolver(func(target testResource) (Resource, bool) {
		return Resource{Kind: "doc", ID: target.ID}, true
	}))
	require.NoError(t, err)

	newCtx := func() context.Context {
		ctx := context.Background()
		ctx, err = WithPrincipal(ctx, Principal{SubjectID: 9})
		require.NoError(t, err)
		ctx, err = WithConsistencyMode(ctx, ConsistencyModeBoundedStaleness)
		require.NoError(t, err)
		ctx, err = WithSnapshotResolver(ctx, resolver)
		require.NoError(t, err)
		ctx, err = WithAuthzLogStore(ctx, logStore)
		require.NoError(t, err)
		ctx, err = WithAuthzMetrics(ctx, metrics)
		require.NoError(t, err)
		return ctx
	}

	decision, err := authorizer.Authorize(newCtx(), "doc:read", testResource{ID: "doc-1"})
	require.NoError(t, err)
	require.Equal(t, "snap-v1", decision.SnapshotVersion)

	decision2, err := authorizer.Authorize(newCtx(), "doc:read", testResource{ID: "doc-1"})
	require.NoError(t, err)
	require.Equal(t, "snap-v1", decision2.SnapshotVersion)

	entries, err := logStore.ListAuthzLogEntries(context.Background(), AuthzLogEntryTypeDecision, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "doc:read", entries[0].Permission)
	require.True(t, entries[0].CacheHit)
	require.False(t, entries[1].CacheHit)
	require.Equal(t, "9", entries[0].PrincipalID)
	require.Equal(t, "snap-v1", entries[0].SnapshotVersion)

	require.Equal(t, int64(1), metrics.CounterValue("authz_decision_total", map[string]string{
		"permission": "doc:read",
		"effect":     "allow",
		"cache_hit":  "false",
	}))
	require.Equal(t, int64(1), metrics.CounterValue("authz_decision_total", map[string]string{
		"permission": "doc:read",
		"effect":     "allow",
		"cache_hit":  "true",
	}))
}

func TestRecordWriteConstraintAudit_UsesConstraintMetadataWhenContextMissing(t *testing.T) {
	logStore := NewMemoryAuthzLogStore()
	ctx, err := WithAuthzLogStore(context.Background(), logStore)
	require.NoError(t, err)

	RecordWriteAudit(ctx, "update", WriteGuard{
		DecisionID:      "decision-from-constraint",
		SnapshotVersion: "snap-from-constraint",
		Consistency:     ConsistencyModeBoundedStaleness,
		Resources:       []ResourceWriteGuard{{Kind: "doc", ResourceID: "doc-1"}},
	}, ResourceWriteGuard{Kind: "doc", ResourceID: "doc-1"})

	entries, err := logStore.ListAuthzLogEntries(context.Background(), AuthzLogEntryTypeWrite, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "decision-from-constraint", entries[0].DecisionID)
	require.Equal(t, "snap-from-constraint", entries[0].SnapshotVersion)
	require.Equal(t, ConsistencyModeBoundedStaleness, entries[0].Consistency)
}
