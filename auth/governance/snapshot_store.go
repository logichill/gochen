package governance

import (
	"context"
	"fmt"
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
// 诊断必须随单次解析结果返回：CacheHit 是“这一次解析”的属性，
// 不是解析器实例的属性。若做成实例字段，并发请求会互相覆盖，
// 让授权审计日志里的命中率统计失真。
type snapshotDiagnostics interface {
	CacheHit() bool
	SnapshotKey() string
}

// snapshotResultDiagnostics 让单次解析结果携带诊断信息。
// 优先于 snapshotDiagnostics：实现该接口时审计层从解析返回值读取，
// 而非从共享实例字段读取。
type snapshotResultDiagnostics interface {
	SnapshotCacheHit() bool
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
func (s *storeBackedSnapshot) ResolveSnapshot(ctx context.Context) (snap PolicySnapshot, err error) {
	s.mu.Lock()
	if s.hasCache && s.ttl > 0 && s.clock.Now().Sub(s.cachedAt) < s.ttl {
		s.lastHit = true
		snapshot := s.cached
		s.mu.Unlock()
		hitSnap := snapshot.WithSnapshotCacheHit(true)
		if hitSnap.Key == "" {
			hitSnap.Key = s.key
		}
		recordSnapshotCapture(ctx, hitSnap)
		return hitSnap, nil
	}
	s.lastHit = false
	if s.inflight != nil {
		load := s.inflight
		s.mu.Unlock()
		snap, err = load.wait(ctx)
		if err == nil {
			if snap.Key == "" {
				snap.Key = s.key
			}
			recordSnapshotCapture(ctx, snap)
		}
		return snap, err
	}
	load := &snapshotLoad{done: make(chan struct{})}
	s.inflight = load
	s.mu.Unlock()

	// 无论回源正常返回还是 panic，都必须清理在途状态并关闭 done：
	// 否则回源 panic 会让 inflight 永远残留、done 永不关闭，所有后续
	// 并发与后续请求永久死锁挂起。回源 panic 转换为内部错误向上传播，
	// 等待者拿到 fail-closed 错误，绝不缓存任何未知状态。
	defer func() {
		if r := recover(); r != nil {
			load.err = errors.NewCode(errors.Internal, "policy snapshot load panicked").
				WithContext("panic", fmt.Sprint(r))
		}
		s.mu.Lock()
		if s.inflight == load {
			s.inflight = nil
		}
		if load.err == nil {
			s.cached = load.snap
			s.cachedAt = s.clock.Now()
			s.hasCache = true
		}
		s.mu.Unlock()
		close(load.done)
		snap, err = load.snap, load.err
		if err == nil {
			if snap.Key == "" {
				snap.Key = s.key
			}
			recordSnapshotCapture(ctx, snap)
		}
	}()

	load.snap, load.err = s.load(ctx)
	if load.err == nil {
		// 回源成功属于 cache miss，显式标注（false 是零值，但写清楚语义）。
		load.snap = load.snap.WithSnapshotCacheHit(false)
		if load.snap.Key == "" {
			load.snap.Key = s.key
		}
	}
	return load.snap, load.err
}

// CacheHit 返回最近一次 ResolveSnapshot 是否直接命中 TTL 缓存（诊断用）。
//
// Deprecated: 该值是实例级的，并发解析时会被其它请求覆盖。
// 审计层应改用解析返回值上的 snapshotResultDiagnostics，仅当快照类型
// 不支持时才回退到这里。保留该方法以兼容自定义实现。
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
