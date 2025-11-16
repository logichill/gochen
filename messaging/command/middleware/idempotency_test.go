package middleware

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/messaging"
	"gochen/messaging/command"
)

// TestIdempotencyMiddleware_FirstExecution 测试首次执行
func TestIdempotencyMiddleware_FirstExecution(t *testing.T) {
	config := &IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: time.Minute,
	}
	middleware := NewIdempotencyMiddleware(config)
	defer middleware.Stop()

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)
	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
	assert.Equal(t, 1, middleware.GetProcessedCount())
}

// TestIdempotencyMiddleware_DuplicateExecution 测试重复执行
func TestIdempotencyMiddleware_DuplicateExecution(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)

	// 第一次执行
	err := middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)

	// 第二次执行（重复）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount) // 不应该再次执行
}

// TestIdempotencyMiddleware_TTLExpiration 测试 TTL 过期
func TestIdempotencyMiddleware_TTLExpiration(t *testing.T) {
	config := &IdempotencyConfig{
		TTL:             50 * time.Millisecond,
		CleanupInterval: 20 * time.Millisecond,
	}
	middleware := NewIdempotencyMiddleware(config)
	defer middleware.Stop()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)

	// 第一次执行
	err := middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)

	// 等待 TTL 过期
	time.Sleep(100 * time.Millisecond)

	// 第二次执行（应该允许，因为已过期）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 2, executionCount)
}

// TestIdempotencyMiddleware_FailedCommand 测试失败的命令
func TestIdempotencyMiddleware_FailedCommand(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return assert.AnError // 返回错误
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)

	// 第一次执行（失败）
	err := middleware.Handle(context.Background(), cmd, next)
	assert.Error(t, err)
	assert.Equal(t, 1, executionCount)

	// 第二次执行（应该允许重试，因为第一次失败了）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.Error(t, err)
	assert.Equal(t, 2, executionCount)
}

// TestIdempotencyMiddleware_NoCommandID 测试没有 ID 的命令
func TestIdempotencyMiddleware_NoCommandID(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	// 创建没有 ID 的命令
	cmd := &command.Command{}

	// 应该正常执行（不做幂等性检查）
	err := middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)
}

// TestIdempotencyMiddleware_NonCommandMessage 测试非命令消息
func TestIdempotencyMiddleware_NonCommandMessage(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

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
	assert.Equal(t, 0, middleware.GetProcessedCount())
}

// TestIdempotencyMiddleware_ConcurrentAccess 测试并发访问
func TestIdempotencyMiddleware_ConcurrentAccess(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	var executionCount int32
	var mu sync.Mutex
	next := func(ctx context.Context, msg messaging.IMessage) error {
		mu.Lock()
		executionCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)

	// 并发执行相同命令
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = middleware.Handle(context.Background(), cmd, next)
		}()
	}

	wg.Wait()

	// 应该只执行一次
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int32(1), executionCount)
}

// TestIdempotencyMiddleware_Clear 测试清空
func TestIdempotencyMiddleware_Clear(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)
	_ = middleware.Handle(context.Background(), cmd, next)

	assert.Equal(t, 1, middleware.GetProcessedCount())

	middleware.Clear()
	assert.Equal(t, 0, middleware.GetProcessedCount())
}

// TestIdempotencyMiddleware_Name 测试中间件名称
func TestIdempotencyMiddleware_Name(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer middleware.Stop()

	assert.Equal(t, "CommandIdempotency", middleware.Name())
}
