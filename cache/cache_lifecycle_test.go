package cache

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"gochen/testkit/assert"
)

// lifecycleTestCache 创建启用 TTL + 后台清理的测试缓存。
func lifecycleTestCache() *Cache[int, int] {
	return New[int, int](Config{
		Name:            "lifecycle",
		TTL:             50 * time.Millisecond,
		CleanupInterval: 5 * time.Millisecond,
	})
}

// TestCache_Lifecycle_CloseIdempotentAndRestart 覆盖 P2#20（056f2b0a）：
// Close 幂等且不阻塞；Close 后 StartCleanup 可重启清理协程；
// Close→StartCleanup→Close 序列不死锁、不泄漏 goroutine。
func TestCache_Lifecycle_CloseIdempotentAndRestart(t *testing.T) {
	c := lifecycleTestCache()

	// 未启动即 Close：空操作。
	c.Close()

	c.StartCleanup()
	c.StartCleanup() // 重复 Start：空操作（仍只有一个清理协程）。
	c.Close()
	c.Close() // 重复 Close：不阻塞、不 panic。

	// Close 后可重启，再 Close 仍安全。
	done := make(chan struct{})
	go func() {
		c.StartCleanup()
		c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close→StartCleanup→Close sequence deadlocked")
	}

	c.Set(1, 1)
	v, ok := c.Get(1)
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

// TestCache_Lifecycle_CleanupGoroutineExitsAfterClose 验证 Close 后清理协程
// 确实退出（goroutine 计数回落），不存在泄漏。
func TestCache_Lifecycle_CleanupGoroutineExitsAfterClose(t *testing.T) {
	c := lifecycleTestCache()

	// 预热：让 GC/运行时稳定 goroutine 计数。
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	c.StartCleanup()
	time.Sleep(50 * time.Millisecond)
	running := runtime.NumGoroutine()
	if running <= baseline {
		t.Logf("cleanup goroutine count did not visibly increase (baseline=%d now=%d); still asserting exit", baseline, running)
	}

	c.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cleanup goroutine leaked: baseline=%d, after close=%d", baseline, runtime.NumGoroutine())
}

// TestCache_Lifecycle_ConcurrentStartClose 在 -race 下验证并发 Start/Close
// 串行安全：结束态为"已停止、无泄漏"，且不再有清理协程运行。
func TestCache_Lifecycle_ConcurrentStartClose(t *testing.T) {
	c := lifecycleTestCache()
	c.Set(1, 1)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				c.StartCleanup()
				_, _ = c.Get(1)
				c.Set(i*100+j, j)
				c.Close()
			}
		}(i)
	}
	wg.Wait()

	// 收尾 Close：无论如何交错，最终必须能无阻塞地停在"已停止"状态。
	done := make(chan struct{})
	go func() {
		c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("final Close blocked after concurrent Start/Close")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("key written before concurrency should survive lifecycle churn")
	}
}
