package lock

import "context"

// ILockProvider 抽象Lock提供者能力接口。
type ILockProvider interface {
	Acquire(ctx context.Context, key string) (release func(), err error)
}

// ILockLease 表示一次已获取的锁租约。
type ILockLease interface {
	// Release 释放锁；多次调用必须安全。
	Release()
	// Lost 在租约丢失时关闭并返回原因；正常释放时关闭且不返回错误。
	Lost() <-chan error
}

// ILeaseLockProvider 表示可暴露租约丢失信号的锁提供者。
// 嵌入 ILockProvider，使得任何租约锁提供者同时也是基础锁提供者。
type ILeaseLockProvider interface {
	ILockProvider
	AcquireLease(ctx context.Context, key string) (ILockLease, error)
}
