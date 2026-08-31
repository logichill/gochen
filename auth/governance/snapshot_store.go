package governance

import (
	"context"
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
)

// IPolicySnapshotStore 持久化策略快照。
//
// Core 只定义契约；SQL / NoSQL 实现由 runtime 提供
// （见 runtime/auth/sqlstore）。
type IPolicySnapshotStore interface {
	SavePolicySnapshot(ctx context.Context, snapshot PolicySnapshot) error
	LoadPolicySnapshot(ctx context.Context, key string) (*PolicySnapshot, error)
	DeletePolicySnapshot(ctx context.Context, key string) error
	ListPolicySnapshots(ctx context.Context, prefix string, limit int) ([]PolicySnapshot, error)
	CleanupPolicySnapshots(ctx context.Context, retentionPeriod time.Duration) error
}

// snapshotDiagnostics 是快照解析器可暴露的诊断面。
//
// 审计层在快照解析完成后经此可选接口读取 CacheHit 与快照 key，
// 填入 AuthzLogEntry；自定义 IPolicySnapshot 不实现该接口时字段留空。
type snapshotDiagnostics interface {
	CacheHit() bool
	SnapshotKey() string
}

// storeBackedSnapshot 从存储读取策略快照，并在 TTL 内复用同一版本。
//
// 复用是为了保证"快照作用范围"：同一请求/事务内的多次判定必须基于同一策略版本，
// 否则判定依据会在中途漂移。
type storeBackedSnapshot struct {
	store IPolicySnapshotStore
	key   string
	ttl   time.Duration
	clock clock.IClock

	mu       sync.Mutex
	cached   PolicySnapshot
	cachedAt time.Time
	hasCache bool
	// lastHit 表示最近一次 ResolveSnapshot 是否直接命中 TTL 缓存（诊断用）。
	lastHit bool
	// inflight 记录进行中的回源加载；TTL 过期瞬间的并发判定共享同一次加载，
	// 而不是各自持锁回源把整条 L4 授权链串行化在存储 I/O 上。
	inflight *snapshotLoad
}

// snapshotLoad 是一次进行中的回源加载。
//
// 首个发起者负责加载并广播结果；等待者在自己的 ctx 取消时提前返回。
type snapshotLoad struct {
	done chan struct{}
	snap PolicySnapshot
	err  error
}

var _ IPolicySnapshot = (*storeBackedSnapshot)(nil)

// NewStoreBackedSnapshot 创建带 TTL 缓存的存储型快照解析器。
//
// ttl <= 0 表示不缓存，每次判定都回源。
func NewStoreBackedSnapshot(store IPolicySnapshotStore, key string, ttl time.Duration, opts ...SnapshotOption) (IPolicySnapshot, error) {
	if store == nil {
		return nil, errors.NewCode(errors.InvalidInput, "policy snapshot store cannot be nil")
	}
	if key == "" {
		return nil, errors.NewCode(errors.InvalidInput, "policy snapshot key cannot be empty")
	}
	snapshot := &storeBackedSnapshot{store: store, key: key, ttl: ttl, clock: clock.NewRealClock()}
	for _, opt := range opts {
		if opt != nil {
			opt(snapshot)
		}
	}
	return snapshot, nil
}

// SnapshotOption 配置存储型快照解析器。
type SnapshotOption func(*storeBackedSnapshot)

// WithSnapshotClock 注入时钟（便于测试确定性）。
func WithSnapshotClock(c clock.IClock) SnapshotOption {
	return func(s *storeBackedSnapshot) {
		if c != nil {
			s.clock = c
		}
	}
}

// ResolveSnapshot 返回当前策略快照；TTL 内命中缓存。
//
// 存储中不存在该 key 时返回错误——由 Wrap 的 fail-closed 策略拒绝请求，
// 绝不返回空快照让调用方误以为"没有策略约束"。
//
// TTL 过期瞬间的并发调用共享同一次回源加载（single-flight）；
// 加载失败不缓存，下一次调用会重新回源。
func (s *storeBackedSnapshot) ResolveSnapshot(ctx context.Context) (PolicySnapshot, error) {
	s.mu.Lock()
	if s.hasCache && s.ttl > 0 && s.clock.Now().Sub(s.cachedAt) < s.ttl {
		s.lastHit = true
		snapshot := s.cached
		s.mu.Unlock()
		return snapshot, nil
	}
	s.lastHit = false
	if s.inflight != nil {
		load := s.inflight
		s.mu.Unlock()
		return load.wait(ctx)
	}
	load := &snapshotLoad{done: make(chan struct{})}
	s.inflight = load
	s.mu.Unlock()

	load.snap, load.err = s.load(ctx)

	s.mu.Lock()
	s.inflight = nil
	if load.err == nil {
		s.cached = load.snap
		s.cachedAt = s.clock.Now()
		s.hasCache = true
	}
	s.mu.Unlock()
	close(load.done)
	return load.snap, load.err
}

// CacheHit 返回最近一次 ResolveSnapshot 是否直接命中 TTL 缓存（诊断用）。
//
// 未发生过解析、或最近一次走了回源加载（含共享他人的在途加载）时为 false。
func (s *storeBackedSnapshot) CacheHit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHit
}

// SnapshotKey 返回本解析器绑定的策略快照 key（诊断用）。
func (s *storeBackedSnapshot) SnapshotKey() string {
	return s.key
}

var _ snapshotDiagnostics = (*storeBackedSnapshot)(nil)

func (s *storeBackedSnapshot) load(ctx context.Context) (PolicySnapshot, error) {
	loaded, err := s.store.LoadPolicySnapshot(ctx, s.key)
	if err != nil {
		return PolicySnapshot{}, err
	}
	if loaded == nil {
		return PolicySnapshot{}, errors.NewCode(errors.NotFound, "policy snapshot not found").
			WithContext("snapshot_key", s.key)
	}
	return NormalizeSnapshot(*loaded), nil
}

func (l *snapshotLoad) wait(ctx context.Context) (PolicySnapshot, error) {
	select {
	case <-l.done:
		return l.snap, l.err
	case <-ctx.Done():
		return PolicySnapshot{}, ctx.Err()
	}
}
