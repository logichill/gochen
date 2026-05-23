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
	locks map[string]chan struct{}
}

// NewMemoryLockProvider 创建MemoryLock提供者。
func NewMemoryLockProvider() *MemoryLockProvider {
	return &MemoryLockProvider{
		locks: make(map[string]chan struct{}),
	}
}

func (p *MemoryLockProvider) Acquire(ctx context.Context, key string) (func(), error) {
	if key == "" {
		return nil, errors.NewCode(errors.InvalidInput, "lock key cannot be empty")
	}
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	p.mu.Lock()
	ch, ok := p.locks[key]
	if !ok {
		ch = make(chan struct{}, 1)
		p.locks[key] = ch
	}
	p.mu.Unlock()

	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			select {
			case <-ch:
			default:
			}
		})
	}
	return release, nil
}

var _ ILockProvider = (*MemoryLockProvider)(nil)
