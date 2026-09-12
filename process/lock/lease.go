package lock

import "sync"

// Lease 提供标准的 ILockLease 抽象实现。
type Lease struct {
	releaseOnce sync.Once
	releaseFn   func()
	lost        <-chan error
}

var _ ILockLease = (*Lease)(nil)

// NewLease 创建一个具有幂等释放和丢失通知通道的锁租约。
func NewLease(release func(), lost <-chan error) *Lease {
	if release == nil {
		release = func() {}
	}
	return &Lease{
		releaseFn: release,
		lost:      lost,
	}
}

// Release 释放锁；内部使用 sync.Once 保证多次调用安全。
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		if l.releaseFn != nil {
			l.releaseFn()
		}
	})
}

// Lost 返回租约丢失通知 channel；当 l 为 nil 或 lost 为 nil 时返回已关闭的 channel。
func (l *Lease) Lost() <-chan error {
	if l == nil || l.lost == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return l.lost
}

// SafeLostChan 提供线程安全的租约丢失通知分发及单次关闭语义。
type SafeLostChan struct {
	ch   chan error
	once sync.Once
}

// NewSafeLostChan 创建容量为 1 的安全丢失通知 channel。
func NewSafeLostChan() *SafeLostChan {
	return &SafeLostChan{
		ch: make(chan error, 1),
	}
}

// Channel 返回只读丢失通知通道。
func (s *SafeLostChan) Channel() <-chan error {
	if s == nil || s.ch == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return s.ch
}

// Close 单次关闭 channel，并在 err != nil 时尝试非阻塞写入错误原因。
func (s *SafeLostChan) Close(err error) {
	if s == nil || s.ch == nil {
		return
	}
	s.once.Do(func() {
		if err != nil {
			select {
			case s.ch <- err:
			default:
			}
		}
		close(s.ch)
	})
}
