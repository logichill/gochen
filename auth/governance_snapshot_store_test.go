package auth_test

import (
	"context"
	auth "gochen/auth"
	"gochen/clock"
	"gochen/errors"
	"gochen/testkit/require"
	"sync"
	"testing"
	"time"
)

// manualClock 是可手动推进的测试时钟。
type manualClock struct {
	clock.RealClock
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// gatedSnapshotStore 允许阻塞 LoadPolicySnapshot 并统计回源次数。
type gatedSnapshotStore struct {
	mu    sync.Mutex
	loads int
	snap  *auth.PolicySnapshot
	err   error
	gate  chan struct{}
}

func (s *gatedSnapshotStore) SavePolicySnapshot(context.Context, auth.PolicySnapshot) error {
	return nil
}

func (s *gatedSnapshotStore) LoadPolicySnapshot(context.Context, string) (*auth.PolicySnapshot, error) {
	s.mu.Lock()
	s.loads++
	gate := s.gate
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return s.snap, s.err
}

func (s *gatedSnapshotStore) DeletePolicySnapshot(context.Context, string) error { return nil }

func (s *gatedSnapshotStore) ListPolicySnapshots(context.Context, string, int) ([]auth.PolicySnapshot, error) {
	return nil, nil
}

func (s *gatedSnapshotStore) CleanupPolicySnapshots(context.Context, time.Duration) error { return nil }

func (s *gatedSnapshotStore) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

// armGate 在此后的回源加载上挂一道闸，返回用于放行的通道。
func (s *gatedSnapshotStore) armGate() chan struct{} {
	gate := make(chan struct{})
	s.mu.Lock()
	s.gate = gate
	s.mu.Unlock()
	return gate
}

// TTL 内命中缓存，过期后回源并更新缓存版本。
func TestStoreBackedSnapshotCachesWithinTTL(t *testing.T) {
	store := &gatedSnapshotStore{snap: &auth.PolicySnapshot{Key: "k", Version: "v1"}}
	current := &auth.PolicySnapshot{Key: "k", Version: "v2"}
	snap, err := auth.NewStoreBackedSnapshot(store, "k", time.Minute,
		auth.WithSnapshotClock(&manualClock{now: time.Unix(0, 0)}))
	require.NoError(t, err)

	got, err := snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1", got.Version)

	// TTL 内再次解析：命中缓存，不回源。
	got, err = snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1", got.Version)
	require.Equal(t, 1, store.loadCount())

	// 存储侧版本已更新，但 TTL 内仍返回旧版本（共享缓存复用）。
	store.snap = current
	got, err = snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1", got.Version)
}

// 快照不存在时必须报错——绝不返回空快照让调用方误以为没有策略约束。
func TestStoreBackedSnapshotFailsClosedOnMissingKey(t *testing.T) {
	store := &gatedSnapshotStore{}
	snap, err := auth.NewStoreBackedSnapshot(store, "k", time.Minute)
	require.NoError(t, err)

	_, err = snap.ResolveSnapshot(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.NotFound))
}

// TTL 过期瞬间的并发解析必须共享同一次回源加载，
// 而不是各自把整条 L4 授权链串行化在存储 I/O 上。
func TestStoreBackedSnapshotSingleFlight(t *testing.T) {
	store := &gatedSnapshotStore{
		snap: &auth.PolicySnapshot{Key: "k", Version: "v1"},
	}
	mc := &manualClock{now: time.Unix(0, 0)}
	snap, err := auth.NewStoreBackedSnapshot(store, "k", time.Minute,
		auth.WithSnapshotClock(mc))
	require.NoError(t, err)

	// 预热缓存。
	_, err = snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)

	// 挂闸后再并发，回源会停在存储 I/O 上，直到主测试放行。
	gate := store.armGate()

	const callers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got, err := snap.ResolveSnapshot(context.Background())
			if err == nil {
				results[i] = got.Version
			}
		}(i)
	}
	// TTL 过期后并发发起，同时放行回源。
	mc.advance(2 * time.Minute)
	close(start)
	close(gate)
	wg.Wait()

	require.Equal(t, 2, store.loadCount(), "并发解析必须共享一次回源加载（预热 1 次 + 共享回源 1 次）")
	for _, version := range results {
		require.Equal(t, "v1", version)
	}
}

// CacheHit 语义：回源加载为 false，TTL 命中为 true。
func TestStoreBackedSnapshotCacheHitDiagnostics(t *testing.T) {
	store := &gatedSnapshotStore{snap: &auth.PolicySnapshot{Key: "k", Version: "v1"}}
	mc := &manualClock{now: time.Unix(0, 0)}
	snap, err := auth.NewStoreBackedSnapshot(store, "k", time.Minute,
		auth.WithSnapshotClock(mc))
	require.NoError(t, err)

	first, err := snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.False(t, first.SnapshotCacheHit(), "首次解析必须回源")

	second, err := snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.True(t, second.SnapshotCacheHit(), "TTL 内再次解析应命中缓存")

	mc.advance(2 * time.Minute)
	third, err := snap.ResolveSnapshot(context.Background())
	require.NoError(t, err)
	require.False(t, third.SnapshotCacheHit(), "TTL 过期后回源")

	require.Equal(t, "k", first.Key)
	require.Equal(t, "k", second.Key)
	require.Equal(t, "k", third.Key)
	require.False(t, first.SnapshotCacheHit(), "后续解析保持前一次结果不变")
}
