package auth

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"gochen/errors"
)

// SnapshotResolution 表示一次策略快照解析结果。
type SnapshotResolution struct {
	Version  string
	Key      string
	CacheHit bool
}

// PolicySnapshot 表示一条持久化的策略快照记录。
type PolicySnapshot struct {
	Key       string
	Version   string
	Timestamp time.Time
	Metadata  map[string]any
}

// IPolicySnapshotStore 定义策略快照持久化接口。
type IPolicySnapshotStore interface {
	SavePolicySnapshot(ctx context.Context, snapshot PolicySnapshot) error
	LoadPolicySnapshot(ctx context.Context, key string) (*PolicySnapshot, error)
	DeletePolicySnapshot(ctx context.Context, key string) error
	ListPolicySnapshots(ctx context.Context, prefix string, limit int) ([]PolicySnapshot, error)
	CleanupPolicySnapshots(ctx context.Context, retentionPeriod time.Duration) error
}

// IPolicySnapshotKeyResolver 定义快照 key 解析接口。
type IPolicySnapshotKeyResolver interface {
	ResolvePolicySnapshotKey(ctx context.Context, eval AuthzEvalContext) (string, error)
}

// PolicySnapshotKeyResolverFunc 允许使用函数直接实现快照 key 解析。
type PolicySnapshotKeyResolverFunc func(ctx context.Context, eval AuthzEvalContext) (string, error)

// ResolvePolicySnapshotKey 解析快照 key。
func (f PolicySnapshotKeyResolverFunc) ResolvePolicySnapshotKey(ctx context.Context, eval AuthzEvalContext) (string, error) {
	return f(ctx, eval)
}

// StaticPolicySnapshotKeyResolver 返回固定 key 的解析器。
func StaticPolicySnapshotKeyResolver(key string) PolicySnapshotKeyResolverFunc {
	return func(ctx context.Context, eval AuthzEvalContext) (string, error) {
		_ = ctx
		_ = eval
		key = strings.TrimSpace(key)
		if key == "" {
			return "", errors.NewCode(errors.InvalidInput, "policy snapshot key is required")
		}
		return key, nil
	}
}

// DefaultPolicySnapshotKeyResolver 返回默认 key 解析器。
func DefaultPolicySnapshotKeyResolver() PolicySnapshotKeyResolverFunc {
	return StaticPolicySnapshotKeyResolver("default")
}

// MemoryPolicySnapshotStore 表示内存策略快照存储。
type MemoryPolicySnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]PolicySnapshot
}

// NewMemoryPolicySnapshotStore 创建内存策略快照存储。
func NewMemoryPolicySnapshotStore() *MemoryPolicySnapshotStore {
	return &MemoryPolicySnapshotStore{
		snapshots: make(map[string]PolicySnapshot),
	}
}

// SavePolicySnapshot 保存策略快照。
func (s *MemoryPolicySnapshotStore) SavePolicySnapshot(ctx context.Context, snapshot PolicySnapshot) error {
	_ = ctx
	snapshot, err := normalizePolicySnapshot(snapshot)
	if err != nil {
		return err
	}
	snapshot = clonePolicySnapshot(snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.Key] = snapshot
	return nil
}

// LoadPolicySnapshot 加载策略快照。
func (s *MemoryPolicySnapshotStore) LoadPolicySnapshot(ctx context.Context, key string) (*PolicySnapshot, error) {
	_ = ctx
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.NewCode(errors.InvalidInput, "policy snapshot key is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[key]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "policy snapshot not found").WithContext("snapshot_key", key)
	}
	cloned := clonePolicySnapshot(snapshot)
	return &cloned, nil
}

// DeletePolicySnapshot 删除策略快照。
func (s *MemoryPolicySnapshotStore) DeletePolicySnapshot(ctx context.Context, key string) error {
	_ = ctx
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.NewCode(errors.InvalidInput, "policy snapshot key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.snapshots, key)
	return nil
}

// ListPolicySnapshots 列出策略快照。
func (s *MemoryPolicySnapshotStore) ListPolicySnapshots(ctx context.Context, prefix string, limit int) ([]PolicySnapshot, error) {
	_ = ctx
	prefix = strings.TrimSpace(prefix)
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]PolicySnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		if prefix != "" && !strings.HasPrefix(snapshot.Key, prefix) {
			continue
		}
		result = append(result, clonePolicySnapshot(snapshot))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].Key < result[j].Key
		}
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// CleanupPolicySnapshots 清理过期策略快照。
func (s *MemoryPolicySnapshotStore) CleanupPolicySnapshots(ctx context.Context, retentionPeriod time.Duration) error {
	_ = ctx
	if retentionPeriod <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-retentionPeriod)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, snapshot := range s.snapshots {
		if snapshot.Timestamp.Before(cutoff) {
			delete(s.snapshots, key)
		}
	}
	return nil
}

// DefaultSnapshotLoadTimeout 是 store-backed resolver 在 singleflight loader
// 中执行一次 snapshot 加载的默认硬性超时。防止下游 store 永久挂起导致 sf[key]
// 无法清理、所有后续 caller 被 pin 在同一个僵死 inflight 上。
const DefaultSnapshotLoadTimeout = 30 * time.Second

// StoreBackedSnapshotResolver 表示基于持久化 store 的策略快照解析器。
//
// strong：每次读取 store 中最新版本；并发读取时通过 singleflight 合并，
//
//	避免高 QPS 下对 store 造成 thundering herd；
//
// bounded_staleness：优先读本地 TTL cache，过期后回源刷新。
type StoreBackedSnapshotResolver struct {
	store       IPolicySnapshotStore
	keyResolver IPolicySnapshotKeyResolver
	cacheTTL    time.Duration
	loadTimeout time.Duration
	now         func() time.Time

	mu    sync.RWMutex
	cache map[string]policySnapshotCacheEntry

	sfMu sync.Mutex
	sf   map[string]*snapshotInflightCall
}

type snapshotInflightCall struct {
	done chan struct{}
	res  SnapshotResolution
	err  error
}

type policySnapshotCacheEntry struct {
	snapshot  PolicySnapshot
	expiresAt time.Time
}

// NewStoreBackedSnapshotResolver 创建基于 store 的快照解析器。
func NewStoreBackedSnapshotResolver(
	store IPolicySnapshotStore,
	keyResolver IPolicySnapshotKeyResolver,
	cacheTTL time.Duration,
) (*StoreBackedSnapshotResolver, error) {
	if store == nil {
		return nil, errors.NewCode(errors.InvalidInput, "policy snapshot store cannot be nil")
	}
	if keyResolver == nil {
		keyResolver = DefaultPolicySnapshotKeyResolver()
	}
	if cacheTTL < 0 {
		return nil, errors.NewCode(errors.InvalidInput, "policy snapshot cache TTL cannot be negative")
	}
	return &StoreBackedSnapshotResolver{
		store:       store,
		keyResolver: keyResolver,
		cacheTTL:    cacheTTL,
		loadTimeout: DefaultSnapshotLoadTimeout,
		now:         time.Now,
		cache:       make(map[string]policySnapshotCacheEntry),
		sf:          make(map[string]*snapshotInflightCall),
	}, nil
}

// SetLoadTimeout 覆盖默认 loader 超时。设置为 0 表示使用默认值，负值会被忽略。
func (r *StoreBackedSnapshotResolver) SetLoadTimeout(d time.Duration) {
	if r == nil || d < 0 {
		return
	}
	if d == 0 {
		r.loadTimeout = DefaultSnapshotLoadTimeout
		return
	}
	r.loadTimeout = d
}

// ResolveSnapshot 解析一次带 cache 元信息的策略快照。
func (r *StoreBackedSnapshotResolver) ResolveSnapshot(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error) {
	if r == nil || r.store == nil {
		return SnapshotResolution{}, errors.NewCode(errors.InvalidInput, "policy snapshot resolver store cannot be nil")
	}
	key, err := r.keyResolver.ResolvePolicySnapshotKey(ctx, eval)
	if err != nil {
		return SnapshotResolution{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return SnapshotResolution{}, errors.NewCode(errors.InvalidInput, "policy snapshot key is required")
	}

	if normalizeConsistencyMode(eval.Consistency) == ConsistencyModeBoundedStaleness {
		if snapshot, ok := r.loadCachedSnapshot(key); ok {
			return SnapshotResolution{
				Version:  snapshot.Version,
				Key:      snapshot.Key,
				CacheHit: true,
			}, nil
		}
	}

	return r.loadSnapshotSingleflight(ctx, key)
}

func (r *StoreBackedSnapshotResolver) loadSnapshotSingleflight(ctx context.Context, key string) (SnapshotResolution, error) {
	r.sfMu.Lock()
	call, exists := r.sf[key]
	if !exists {
		call = &snapshotInflightCall{done: make(chan struct{})}
		r.sf[key] = call
		r.sfMu.Unlock()

		// Leader 用脱离调用方取消语义的 context 执行实际加载，避免任一 caller
		// 的 ctx cancel 污染共享结果——单个 caller 取消只影响自己，不会让其他
		// waiter 看到 ctx.Canceled。此 goroutine 只负责完成 load + 写入 cache，
		// 每个 caller（含最初发起者）都回到下面的 select 各自监听自己的 ctx。
		go r.runSnapshotLoad(ctx, key, call)
	} else {
		r.sfMu.Unlock()
	}

	select {
	case <-call.done:
		return call.res, call.err
	case <-ctx.Done():
		return SnapshotResolution{}, ctx.Err()
	}
}

func (r *StoreBackedSnapshotResolver) runSnapshotLoad(ctx context.Context, key string, call *snapshotInflightCall) {
	defer func() {
		r.sfMu.Lock()
		delete(r.sf, key)
		r.sfMu.Unlock()
		close(call.done)
	}()

	loadCtx, cancel := r.newLoadContext(ctx)
	defer cancel()

	snapshot, err := r.store.LoadPolicySnapshot(loadCtx, key)
	if err != nil {
		call.err = err
		return
	}
	if snapshot == nil {
		call.err = errors.NewCode(errors.NotFound, "policy snapshot not found").WithContext("snapshot_key", key)
		return
	}
	if _, err := normalizePolicySnapshot(*snapshot); err != nil {
		call.err = err
		return
	}
	r.storeCachedSnapshot(*snapshot)
	call.res = SnapshotResolution{Version: snapshot.Version, Key: snapshot.Key, CacheHit: false}
}

// newLoadContext 返回与 caller ctx 的取消/截止时间解耦、但仍有统一 deadline 兜底的 ctx。
//
// 共享 singleflight load 只受 resolver 自己的 loadTimeout 约束，避免首个 caller
// 的更短 deadline 过早终止共享结果；同时依旧保证下游 store 卡住时 sf[key]
// 会在上限时间内被清理。
func (r *StoreBackedSnapshotResolver) newLoadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	loadCtx := context.WithoutCancel(ctx)
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	timeout := r.loadTimeout
	if timeout <= 0 {
		timeout = DefaultSnapshotLoadTimeout
	}
	return context.WithDeadline(loadCtx, now().Add(timeout))
}

func (r *StoreBackedSnapshotResolver) loadCachedSnapshot(key string) (PolicySnapshot, bool) {
	if r == nil || r.cacheTTL <= 0 {
		return PolicySnapshot{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.cache[key]
	if !ok {
		return PolicySnapshot{}, false
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	if !entry.expiresAt.IsZero() && now().After(entry.expiresAt) {
		return PolicySnapshot{}, false
	}
	return entry.snapshot, true
}

func (r *StoreBackedSnapshotResolver) storeCachedSnapshot(snapshot PolicySnapshot) {
	if r == nil || r.cacheTTL <= 0 {
		return
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[snapshot.Key] = policySnapshotCacheEntry{
		snapshot:  snapshot,
		expiresAt: now().Add(r.cacheTTL),
	}
}

func normalizePolicySnapshot(snapshot PolicySnapshot) (PolicySnapshot, error) {
	snapshot.Key = strings.TrimSpace(snapshot.Key)
	snapshot.Version = strings.TrimSpace(snapshot.Version)
	if snapshot.Key == "" {
		return PolicySnapshot{}, errors.NewCode(errors.InvalidInput, "policy snapshot key is required")
	}
	if snapshot.Version == "" {
		return PolicySnapshot{}, errors.NewCode(errors.InvalidInput, "policy snapshot version is required")
	}
	if snapshot.Timestamp.IsZero() {
		snapshot.Timestamp = time.Now()
	}
	if len(snapshot.Metadata) == 0 {
		snapshot.Metadata = nil
	}
	return snapshot, nil
}

func clonePolicySnapshot(snapshot PolicySnapshot) PolicySnapshot {
	snapshot.Metadata = clonePolicySnapshotMetadata(snapshot.Metadata)
	return snapshot
}

func clonePolicySnapshotMetadata(metadata map[string]any) map[string]any {
	return cloneMetadataMap(metadata)
}
