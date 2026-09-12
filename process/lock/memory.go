package lock

import (
	"context"
	"sync"

	"gochen/errors"
)

// MemoryLockProvider 基于进程内 map+channel 的锁实现。
//
// 注意：
// - 该实现不具备跨进程/跨实例能力，仅适用于单进程或测试场景；
// - 若需要多实例串行化，应使用 SQL/Redis/Etcd 等分布式实现。
type MemoryLockProvider struct {
	mu    sync.Mutex
	locks map[string]*memoryLock
}

type memoryLock struct {
	ch      chan struct{}
	waiters int
}

// NewMemoryLockProvider 创建MemoryLock提供者。
func NewMemoryLockProvider() *MemoryLockProvider {
	return &MemoryLockProvider{
		locks: make(map[string]*memoryLock),
	}
}

func (p *MemoryLockProvider) Acquire(ctx context.Context, key string) (func(), error) {
	lease, err := p.AcquireLease(ctx, key)
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}

func (p *MemoryLockProvider) AcquireLease(ctx context.Context, key string) (ILockLease, error) {
	if key == "" {
		return nil, errors.NewCode(errors.InvalidInput, "lock key cannot be empty")
	}
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	entry, ok := p.locks[key]
	if !ok {
		entry = &memoryLock{ch: make(chan struct{}, 1)}
		p.locks[key] = entry
	}
	entry.waiters++
	ch := entry.ch
	p.mu.Unlock()

	acquired := false
	select {
	case ch <- struct{}{}:
		acquired = true
	case <-ctx.Done():
		p.releaseWaiter(key, entry, false)
		return nil, ctx.Err()
	}

	lost := make(chan error)
	release := func() {
		p.releaseWaiter(key, entry, acquired)
		close(lost)
	}
	return NewLease(release, lost), nil
}

func (p *MemoryLockProvider) releaseWaiter(key string, entry *memoryLock, acquired bool) {
	if acquired {
		// 先归还 channel token，再在锁内更新 waiter 计数；Acquire 只会在持有同一个 entry 后等待该 token。
		select {
		case <-entry.ch:
		default:
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry.waiters--
	if entry.waiters <= 0 && len(entry.ch) == 0 && p.locks[key] == entry {
		delete(p.locks, key)
	}
}

var _ ILockProvider = (*MemoryLockProvider)(nil)
var _ ILeaseLockProvider = (*MemoryLockProvider)(nil)
