package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/messaging"
	"gochen/messaging/command"
)

// TestAggregateLockMiddleware_SingleAggregate 测试单个聚合串行执行
func TestAggregateLockMiddleware_SingleAggregate(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	var executionOrder []int
	var mu sync.Mutex

	next := func(ctx context.Context, msg messaging.IMessage) error {
		mu.Lock()
		order := len(executionOrder) + 1
		executionOrder = append(executionOrder, order)
		mu.Unlock()

		time.Sleep(20 * time.Millisecond) // 模拟处理时间
		return nil
	}

	aggregateID := int64(123)

	// 并发执行相同聚合的命令
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := command.NewCommand("cmd-"+string(rune(idx)), "TestCommand", aggregateID, "Test", nil)
			_ = middleware.Handle(context.Background(), cmd, next)
		}(i)
	}

	wg.Wait()

	// 验证串行执行（顺序执行）
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 5, len(executionOrder))
	assert.Equal(t, []int{1, 2, 3, 4, 5}, executionOrder)
}

// TestAggregateLockMiddleware_MultipleAggregates 测试多个聚合并行执行
func TestAggregateLockMiddleware_MultipleAggregates(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	var counter int64
	next := func(ctx context.Context, msg messaging.IMessage) error {
		atomic.AddInt64(&counter, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	// 并发执行不同聚合的命令
	var wg sync.WaitGroup
	numAggregates := 10
	for i := 0; i < numAggregates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := command.NewCommand("cmd-"+string(rune(idx)), "TestCommand", int64(idx), "Test", nil)
			_ = middleware.Handle(context.Background(), cmd, next)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(numAggregates), atomic.LoadInt64(&counter))
	assert.Equal(t, numAggregates, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_NoAggregateID 测试没有聚合 ID 的命令
func TestAggregateLockMiddleware_NoAggregateID(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	// 创建没有聚合 ID 的命令
	cmd := command.NewCommand("cmd-1", "TestCommand", 0, "Test", nil)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
	assert.Equal(t, 0, middleware.GetLockCount()) // 不应该创建锁
}

// TestAggregateLockMiddleware_NonCommandMessage 测试非命令消息
func TestAggregateLockMiddleware_NonCommandMessage(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	msg := &messaging.Message{
		ID:   "msg-1",
		Type: messaging.MessageTypeEvent,
	}

	err := middleware.Handle(context.Background(), msg, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
}

// TestAggregateLockMiddleware_Clear 测试清空锁
func TestAggregateLockMiddleware_Clear(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	// 创建几个锁
	for i := 1; i <= 5; i++ {
		cmd := command.NewCommand("cmd-"+string(rune(i)), "TestCommand", int64(i), "Test", nil)
		_ = middleware.Handle(context.Background(), cmd, next)
	}

	assert.Equal(t, 5, middleware.GetLockCount())

	middleware.Clear()
	assert.Equal(t, 0, middleware.GetLockCount())
}

// TestAggregateLockMiddleware_Name 测试中间件名称
func TestAggregateLockMiddleware_Name(t *testing.T) {
	middleware := NewAggregateLockMiddleware(nil)
	assert.Equal(t, "CommandAggregateLock", middleware.Name())
}

// BenchmarkAggregateLockMiddleware_SameAggregate 相同聚合性能测试
func BenchmarkAggregateLockMiddleware_SameAggregate(b *testing.B) {
	middleware := NewAggregateLockMiddleware(nil)
	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	cmd := command.NewCommand("cmd-1", "TestCommand", 123, "Test", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = middleware.Handle(ctx, cmd, next)
	}
}

// BenchmarkAggregateLockMiddleware_DifferentAggregates 不同聚合性能测试
func BenchmarkAggregateLockMiddleware_DifferentAggregates(b *testing.B) {
	middleware := NewAggregateLockMiddleware(nil)
	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := command.NewCommand("cmd-"+string(rune(i)), "TestCommand", int64(i), "Test", nil)
		_ = middleware.Handle(ctx, cmd, next)
	}
}
