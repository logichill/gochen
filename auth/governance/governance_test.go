package governance_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/auth/governance"
	"gochen/testkit/require"
)

type threadSafeAuditStore struct {
	mu      sync.Mutex
	entries []governance.AuthzLogEntry
}

func (s *threadSafeAuditStore) SaveAuthzLogEntry(_ context.Context, entry governance.AuthzLogEntry) error {
	s.mu.Lock()
	s.entries = append(s.entries, entry)
	s.mu.Unlock()
	return nil
}

func (s *threadSafeAuditStore) ListAuthzLogEntries(_ context.Context, _ governance.AuthzLogEntryType, _ int) ([]governance.AuthzLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries, nil
}

// TestAuditAccurateCacheHitPerRequest 验证基于 context 的快照诊断捕获机制：
// 审计条目的 CacheHit 严格记录单次请求的快照解析结果，不受实例级状态并发覆盖的影响。
func TestAuditAccurateCacheHitPerRequest(t *testing.T) {
	snapshotStore := &gatedSnapshotStore{snap: &governance.PolicySnapshot{Key: "policy-key", Version: "v1"}}
	mc := &manualClock{now: time.Unix(0, 0)}
	snapshot, err := governance.NewStoreBackedSnapshot(snapshotStore, "policy-key", time.Minute,
		governance.WithSnapshotClock(mc))
	require.NoError(t, err)

	auditStore := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(snapshot),
		governance.WithAudit(auditStore),
	)
	require.NoError(t, err)

	// 第一次请求：冷启动回源，应当 CacheHit == false
	_, err = wrapped.Authorize(context.Background(), "order:read")
	require.NoError(t, err)
	require.Equal(t, 1, len(auditStore.entries))
	require.False(t, auditStore.entries[0].CacheHit, "首次请求必须记录 CacheHit=false (cache miss)")
	require.Equal(t, "v1", auditStore.entries[0].SnapshotVersion)
	require.Equal(t, "policy-key", auditStore.entries[0].SnapshotKey)

	// 第二次请求：TTL 内，应当 CacheHit == true
	_, err = wrapped.Authorize(context.Background(), "order:read")
	require.NoError(t, err)
	require.Equal(t, 2, len(auditStore.entries))
	require.True(t, auditStore.entries[1].CacheHit, "TTL 内再次请求必须记录 CacheHit=true (cache hit)")
	require.Equal(t, "v1", auditStore.entries[1].SnapshotVersion)
	require.Equal(t, "policy-key", auditStore.entries[1].SnapshotKey)
}

// TestAuditConcurrentCacheHitIsolation 验证并发请求下 CacheHit 诊断互不干扰。
func TestAuditConcurrentCacheHitIsolation(t *testing.T) {
	snapshotStore := &gatedSnapshotStore{snap: &governance.PolicySnapshot{Key: "policy-conc", Version: "v1"}}
	mc := &manualClock{now: time.Unix(0, 0)}
	snapshot, err := governance.NewStoreBackedSnapshot(snapshotStore, "policy-conc", time.Minute,
		governance.WithSnapshotClock(mc))
	require.NoError(t, err)

	auditStore := &threadSafeAuditStore{}

	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(snapshot),
		governance.WithAudit(auditStore),
	)
	require.NoError(t, err)

	// 预热缓存（产生一次 cache miss）
	_, err = wrapped.Authorize(context.Background(), "order:read")
	require.NoError(t, err)

	// 并发执行 20 次请求，全部应在 TTL 内命中缓存
	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, authErr := wrapped.Authorize(context.Background(), "order:read")
			if authErr != nil {
				t.Errorf("authorize failed: %v", authErr)
			}
		}()
	}
	wg.Wait()

	auditStore.mu.Lock()
	defer auditStore.mu.Unlock()
	require.Equal(t, concurrency+1, len(auditStore.entries))

	// 第一次是 miss
	require.False(t, auditStore.entries[0].CacheHit, "预热请求必须为 miss")
	// 后续并发的所有请求都必须是 hit
	for i := 1; i <= concurrency; i++ {
		require.True(t, auditStore.entries[i].CacheHit, "并发请求必须准确记录 CacheHit=true")
		require.Equal(t, "v1", auditStore.entries[i].SnapshotVersion)
		require.Equal(t, "policy-conc", auditStore.entries[i].SnapshotKey)
	}
}

// TestAuditDirectSnapshotFuncRecordsCacheHit 验证使用 PolicySnapshotFunc 自定义快照时依然正常传播。
func TestAuditDirectSnapshotFuncRecordsCacheHit(t *testing.T) {
	customSnapshot := governance.PolicySnapshotFunc(func(ctx context.Context) (governance.PolicySnapshot, error) {
		return governance.PolicySnapshot{
			Key:     "custom-key",
			Version: "v1.0",
		}.WithSnapshotCacheHit(true), nil
	})

	auditStore := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(customSnapshot),
		governance.WithAudit(auditStore),
	)
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), "order:read")
	require.NoError(t, err)
	require.Equal(t, 1, len(auditStore.entries))
	require.True(t, auditStore.entries[0].CacheHit)
	require.Equal(t, "v1.0", auditStore.entries[0].SnapshotVersion)
	require.Equal(t, "custom-key", auditStore.entries[0].SnapshotKey)
}
