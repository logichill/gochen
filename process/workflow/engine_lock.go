package workflow

import (
	"sync"
)

type keyedLock[K comparable] struct {
	mu   sync.Mutex
	refs int
}

// lockDefinition 获取 definition 级互斥锁，并在释放后清理无人使用的锁对象。
func (e *Engine) lockDefinition(definitionID string) func() {
	return lockKeyed(&e.definitionMu, &e.definitionLocks, definitionID)
}

// lockInstance 获取实例级互斥锁，并在释放后清理无人使用的锁对象。
func (e *Engine) lockInstance(instanceID ID) func() {
	return lockKeyed(&e.instanceMu, &e.instanceLocks, instanceID)
}

func lockKeyed[K comparable](mapMu *sync.Mutex, locks *map[K]*keyedLock[K], key K) func() {
	mapMu.Lock()
	if *locks == nil {
		*locks = map[K]*keyedLock[K]{}
	}
	lock := (*locks)[key]
	if lock == nil {
		lock = &keyedLock[K]{}
		(*locks)[key] = lock
	}
	lock.refs++
	mapMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()

		mapMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(*locks, key)
		}
		mapMu.Unlock()
	}
}
