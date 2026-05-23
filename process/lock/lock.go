package lock

import "context"

// ILockProvider 抽象Lock提供者能力接口。
type ILockProvider interface {
	Acquire(ctx context.Context, key string) (release func(), err error)
}
