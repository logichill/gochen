package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/clock"
	"gochen/messaging"
	"gochen/messaging/command"
)

// TestAggregateLockMiddleware_SingleAggregate 验证 AggregateLockMiddleware SingleAggregate。
func TestAggregateLockMiddleware_SingleAggregate(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	const goroutines = 5
	var totalExecutions int64          // 总执行次数
	var concurrentExecutions int64     // 瞬时并发执行数
	var maxConcurrentExecutions int64  // 观测到的最大并发数

	next := func(ctx context.Context, msg messaging.IMessage) error {
		// 进入 next：并发计数 +1，并尝试记录峰值
		cur := atomic.AddInt64(&concurrentExecutions, 1)
		for {
			old := atomic.LoadInt64(&maxConcurrentExecutions)
			if cur <= old || atomic.CompareAndSwapInt64(&maxConcurrentExecutions, old, cur) {
				break
			}
		}
		atomic.AddInt64(&totalExecutions, 1)

		time.Sleep(20 * time.Millisecond) // 延长执行窗口，放大并发检测概率

		atomic.AddInt64(&concurrentExecutions, -1)
		return nil
	}

	aggregateID := "123"

	// 并发触发相同聚合的命令
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := command.NewCommand(fmt.Sprintf("cmd-%d", idx), "TestCommand", aggregateID, "Test", nil)
			_ = middleware.Handle(context.Background(), cmd, next)
		}(i)
	}

	wg.Wait()

	// 所有命令都必须被执行
	assert.Equal(t, int64(goroutines), atomic.LoadInt64(&totalExecutions))
	// 同一聚合命令必须串行：并发峰值不能超过 1
	assert.Equal(t, int64(1), atomic.LoadInt64(&maxConcurrentExecutions),
		"同一聚合的命令应串行执行，不能并发")
}

// TestAggregateLockMiddleware_MultipleAggregates 验证 AggregateLockMiddleware MultipleAggregates。
func TestAggregateLockMiddleware_MultipleAggregates(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	var counter int64
	next := func(ctx context.Context, msg messaging.IMessage) error {
		atomic.AddInt64(&counter, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	// 并发执行不同聚合的命令（从 1 开始，空字符串视为“无聚合 ID”不参与加锁）
	var wg sync.WaitGroup
	numAggregates := 10
	for i := 1; i <= numAggregates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := command.NewCommand(fmt.Sprintf("cmd-%d", idx), "TestCommand", fmt.Sprintf("%d", idx), "Test", nil)
			_ = middleware.Handle(context.Background(), cmd, next)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(numAggregates), atomic.LoadInt64(&counter))
	assert.Equal(t, numAggregates, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_NoAggregateID 验证 AggregateLockMiddleware NoAggregateID。
func TestAggregateLockMiddleware_NoAggregateID(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	// 创建没有聚合 ID 的命令
	cmd := command.NewCommand("cmd-1", "TestCommand", "", "Test", nil)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
	assert.Equal(t, 0, middleware.GetLockCount()) // 不应该创建锁
}

// TestAggregateLockMiddleware_NonCommandMessage 验证 AggregateLockMiddleware NonCommandMessage。
func TestAggregateLockMiddleware_NonCommandMessage(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	msg := &messaging.Message{
		ID:   "msg-1",
		Kind: messaging.KindEvent,
		Type: "TestEvent",
	}

	err := middleware.Handle(context.Background(), msg, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
}

// TestAggregateLockMiddleware_Clear 验证 AggregateLockMiddleware Clear。
func TestAggregateLockMiddleware_Clear(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	// 创建几个锁
	for i := 1; i <= 5; i++ {
		cmd := command.NewCommand(fmt.Sprintf("cmd-%d", i), "TestCommand", fmt.Sprintf("%d", i), "Test", nil)
		_ = middleware.Handle(context.Background(), cmd, next)
	}

	assert.Equal(t, 5, middleware.GetLockCount())

	middleware.Clear()
	assert.Equal(t, 0, middleware.GetLockCount())
}

func TestAggregateLockMiddleware_ClearKeepsActiveAggregateLock(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	secondEntered := make(chan struct{})

	cmd1 := command.NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	cmd2 := command.NewCommand("cmd-2", "TestCommand", "1", "Test", nil)

	go func() {
		_ = middleware.Handle(context.Background(), cmd1, func(ctx context.Context, msg messaging.IMessage) error {
			close(started)
			<-release
			return nil
		})
		close(firstDone)
	}()

	<-started
	middleware.Clear()
	assert.Equal(t, 1, middleware.GetLockCount())

	go func() {
		_ = middleware.Handle(context.Background(), cmd2, func(ctx context.Context, msg messaging.IMessage) error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("expected Clear to keep active aggregate lock until first command finishes")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-firstDone

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second command")
	}
}

func TestAggregateLockMiddleware_ClearKeepsActiveTypeLock(t *testing.T) {
	config := &AggregateLockConfig{LockGranularity: "type"}
	middleware := NewAggregateLockMiddleware(config)

	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	secondEntered := make(chan struct{})

	cmd1 := command.NewCommand("cmd-1", "TestCommand", "1", "Order", nil)
	cmd2 := command.NewCommand("cmd-2", "TestCommand", "2", "Order", nil)

	go func() {
		_ = middleware.Handle(context.Background(), cmd1, func(ctx context.Context, msg messaging.IMessage) error {
			close(started)
			<-release
			return nil
		})
		close(firstDone)
	}()

	<-started
	middleware.Clear()
	assert.Equal(t, 1, middleware.GetLockCount())

	go func() {
		_ = middleware.Handle(context.Background(), cmd2, func(ctx context.Context, msg messaging.IMessage) error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("expected Clear to keep active type lock until first command finishes")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-firstDone

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second command")
	}
}

// TestAggregateLockMiddleware_TypeGranularity_SameType 验证 AggregateLockMiddleware TypeGranularity SameType。
func TestAggregateLockMiddleware_TypeGranularity_SameType(t *testing.T) {
	config := &AggregateLockConfig{LockGranularity: "type"}
	middleware := NewAggregateLockMiddleware(config)

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	// 同一聚合类型，不同 ID
	for i := 1; i <= 5; i++ {
		cmd := command.NewCommand(fmt.Sprintf("cmd-%d", i), "TestCommand", fmt.Sprintf("%d", i), "Order", nil)
		_ = middleware.Handle(context.Background(), cmd, next)
	}

	assert.Equal(t, 1, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_TypeGranularity_MultipleTypes 验证 AggregateLockMiddleware TypeGranularity MultipleTypes。
func TestAggregateLockMiddleware_TypeGranularity_MultipleTypes(t *testing.T) {
	config := &AggregateLockConfig{LockGranularity: "type"}
	middleware := NewAggregateLockMiddleware(config)

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	cmdUser := command.NewCommand("cmd-user-1", "TestCommand", "1", "User", nil)
	cmdOrder := command.NewCommand("cmd-order-1", "TestCommand", "2", "Order", nil)

	_ = middleware.Handle(context.Background(), cmdUser, next)
	_ = middleware.Handle(context.Background(), cmdOrder, next)

	assert.Equal(t, 2, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_Name 验证 AggregateLockMiddleware Name。
func TestAggregateLockMiddleware_Name(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)
	assert.Equal(t, "CommandAggregateLock", middleware.Name())
}

// TestAggregateLockMiddleware_EvictionDoesNotRemoveActiveLock 验证 AggregateLockMiddleware EvictionDoesNotRemoveActiveLock。
func TestAggregateLockMiddleware_EvictionDoesNotRemoveActiveLock(t *testing.T) {
	config := &AggregateLockConfig{
		LockGranularity:   "aggregate",
		MaxAggregateLocks: 1,
		LockIdleTimeout:   0,
	}
	middleware := NewAggregateLockMiddleware(config)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	cmd1 := command.NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	go func() {
		_ = middleware.Handle(context.Background(), cmd1, func(ctx context.Context, msg messaging.IMessage) error {
			close(started)
			<-release
			return nil
		})
		close(done)
	}()

	<-started // 确保锁 1 已被持有

	// 触发创建第二把锁并尝试淘汰
	cmd2 := command.NewCommand("cmd-2", "TestCommand", "2", "Test", nil)
	err := middleware.Handle(context.Background(), cmd2, func(ctx context.Context, msg messaging.IMessage) error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, 2, middleware.GetLockCount())

	// 同一聚合的后续命令应阻塞在旧锁上
	blocked := make(chan struct{})
	cmd3 := command.NewCommand("cmd-3", "TestCommand", "1", "Test", nil)
	go func() {
		_ = middleware.Handle(context.Background(), cmd3, func(ctx context.Context, msg messaging.IMessage) error {
			close(blocked)
			return nil
		})
	}()

	select {
	case <-blocked:
		t.Fatalf("expected command to block on active lock")
	case <-time.After(50 * time.Millisecond):
	}

	// 释放第一把锁，后续命令应继续执行
	close(release)
	<-done

	select {
	case <-blocked:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected command to proceed after release")
	}
}

// TestAggregateLockMiddleware_PruneExpiredSkipsActiveLock 验证 AggregateLockMiddleware PruneExpiredSkipsActiveLock。
func TestAggregateLockMiddleware_PruneExpiredSkipsActiveLock(t *testing.T) {
	const idleTimeout = 20 * time.Millisecond
	mc := clock.NewManualClock(time.Now())

	config := &AggregateLockConfig{
		LockGranularity:   "aggregate",
		MaxAggregateLocks: 0, // 禁用数量淘汰，避免干扰
		LockIdleTimeout:   idleTimeout,
		Clock:             mc,
	}
	middleware := NewAggregateLockMiddleware(config)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	cmd1 := command.NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	go func() {
		_ = middleware.Handle(context.Background(), cmd1, func(ctx context.Context, msg messaging.IMessage) error {
			close(started)
			<-release
			return nil
		})
		close(done)
	}()

	<-started

	// 虽然时间已超过 idle timeout，但锁仍被持有（active > 0），不应被清理
	mc.Advance(idleTimeout + time.Millisecond)
	pruned := middleware.PruneExpired()
	assert.Equal(t, 0, pruned)
	assert.Equal(t, 1, middleware.GetLockCount())

	// 释放锁：releaseAggregateLock 会更新 lastUsed 为 mc.Now()
	close(release)
	<-done

	// 此时 lastUsed 已被 releaseAggregateLock 更新为当前 mc.Now()，
	// 需要再推进超过 idleTimeout 才能让该锁过期
	mc.Advance(idleTimeout + time.Millisecond)
	pruned2 := middleware.PruneExpired()
	assert.Equal(t, 1, pruned2)
	assert.Equal(t, 0, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_EvictsOldestInactiveLocks 验证 AggregateLockMiddleware EvictsOldestInactiveLocks。
func TestAggregateLockMiddleware_EvictsOldestInactiveLocks(t *testing.T) {
	mc := clock.NewManualClock(time.Now())
	config := &AggregateLockConfig{
		LockGranularity:   "aggregate",
		MaxAggregateLocks: 3,
		LockIdleTimeout:   0,
		Clock:             mc,
	}
	middleware := NewAggregateLockMiddleware(config)

	next := func(ctx context.Context, msg messaging.IMessage) error { return nil }
	for i := 1; i <= 5; i++ {
		cmd := command.NewCommand(fmt.Sprintf("cmd-%d", i), "TestCommand", fmt.Sprintf("%d", i), "Test", nil)
		_ = middleware.Handle(context.Background(), cmd, next)
		mc.Advance(time.Millisecond) // 确保每条命令 lastUsed 严格递增，不依赖真实时钟精度
	}

	assert.Equal(t, 3, middleware.GetLockCount())

	_, has1 := middleware.locksByAggregate["1"]
	_, has2 := middleware.locksByAggregate["2"]
	_, has3 := middleware.locksByAggregate["3"]
	_, has4 := middleware.locksByAggregate["4"]
	_, has5 := middleware.locksByAggregate["5"]

	assert.False(t, has1, "lock 1 should have been evicted (oldest)")
	assert.False(t, has2, "lock 2 should have been evicted (second oldest)")
	assert.True(t, has3, "lock 3 should be retained")
	assert.True(t, has4, "lock 4 should be retained")
	assert.True(t, has5, "lock 5 should be retained")
}

// BenchmarkAggregateLockMiddleware_SameAggregate 用于评估 AggregateLockMiddleware SameAggregate 的性能。
func BenchmarkAggregateLockMiddleware_SameAggregate(b *testing.B) {
	middleware := NewAggregateLockMiddleware(nil)
	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	cmd := command.NewCommand("cmd-1", "TestCommand", "123", "Test", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = middleware.Handle(ctx, cmd, next)
	}
}

// BenchmarkAggregateLockMiddleware_DifferentAggregates 用于评估 AggregateLockMiddleware DifferentAggregates 的性能。
func BenchmarkAggregateLockMiddleware_DifferentAggregates(b *testing.B) {
	middleware := NewAggregateLockMiddleware(nil)
	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := command.NewCommand(fmt.Sprintf("cmd-%d", i), "TestCommand", fmt.Sprintf("%d", i), "Test", nil)
		_ = middleware.Handle(ctx, cmd, next)
	}
}
