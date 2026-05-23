package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/clock"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
)

func TestIdempotencyMiddleware_StopNilContextReturnsInvalidInput(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	err := middleware.Stop(nil)
	assert.True(t, errors.Is(err, errors.InvalidInput))
}

func TestIdempotencyMiddleware_PartialConfigUsesDefaultMaxProcessed(t *testing.T) {
	middleware := NewIdempotencyMiddleware(&IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: time.Hour,
	})
	defer func() { _ = middleware.Stop(context.Background()) }()

	assert.Equal(t, defaultMaxProcessed, middleware.maxProcessed)
}

// TestIdempotencyMiddleware_FirstExecution 验证 IdempotencyMiddleware FirstExecution。
func TestIdempotencyMiddleware_FirstExecution(t *testing.T) {
	config := &IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: time.Minute,
	}
	middleware := NewIdempotencyMiddleware(config)
	defer func() { _ = middleware.Stop(context.Background()) }()

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
	assert.Equal(t, 1, middleware.GetProcessedCount())
}

// TestIdempotencyMiddleware_DuplicateExecution 验证 IdempotencyMiddleware DuplicateExecution。
func TestIdempotencyMiddleware_DuplicateExecution(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

	// 第一次执行
	err := middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)

	// 第二次执行（重复）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount) // 不应该再次执行
}

// TestIdempotencyMiddleware_TTLExpiration 验证 IdempotencyMiddleware TTLExpiration。
func TestIdempotencyMiddleware_TTLExpiration(t *testing.T) {
	const ttl = 50 * time.Millisecond
	mc := clock.NewManualClock(time.Now())

	config := &IdempotencyConfig{
		TTL:             ttl,
		CleanupInterval: time.Hour, // 禁用后台 cleanup，避免干扰
		Clock:           mc,
	}
	middleware := NewIdempotencyMiddleware(config)
	defer func() { _ = middleware.Stop(context.Background()) }()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

	// 第一次执行
	err := middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)

	// 推进时钟，使 TTL 过期（isProcessed 基于 clock.Now().Sub(processedAt) > ttl 判断）
	mc.Advance(ttl + time.Millisecond)

	// 第二次执行（应该允许，因为已过期）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.NoError(t, err)
	assert.Equal(t, 2, executionCount)
}

// TestIdempotencyMiddleware_FailedCommand 验证 IdempotencyMiddleware FailedCommand。
func TestIdempotencyMiddleware_FailedCommand(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	executionCount := 0
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return assert.AnError // 返回错误
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

	// 第一次执行（失败）
	err := middleware.Handle(context.Background(), cmd, next)
	assert.Error(t, err)
	assert.Equal(t, 1, executionCount)

	// 第二次执行（应该允许重试，因为第一次失败了）
	err = middleware.Handle(context.Background(), cmd, next)
	assert.Error(t, err)
	assert.Equal(t, 2, executionCount)
}

// TestIdempotencyMiddleware_NoCommandID 验证 IdempotencyMiddleware NoCommandID。
func TestIdempotencyMiddleware_NoCommandID(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

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

// TestIdempotencyMiddleware_NonCommandMessage 验证 IdempotencyMiddleware NonCommandMessage。
func TestIdempotencyMiddleware_NonCommandMessage(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

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
	assert.Equal(t, 0, middleware.GetProcessedCount())
}

// TestIdempotencyMiddleware_ConcurrentAccess 验证 IdempotencyMiddleware ConcurrentAccess。
func TestIdempotencyMiddleware_ConcurrentAccess(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	var executionCount int
	var mu sync.Mutex
	next := func(ctx context.Context, msg messaging.IMessage) error {
		mu.Lock()
		executionCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

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
	assert.Equal(t, 1, executionCount)
}

// TestIdempotencyMiddleware_Clear 验证 IdempotencyMiddleware Clear。
func TestIdempotencyMiddleware_Clear(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	next := func(ctx context.Context, msg messaging.IMessage) error {
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	_ = middleware.Handle(context.Background(), cmd, next)

	assert.Equal(t, 1, middleware.GetProcessedCount())

	middleware.Clear()
	assert.Equal(t, 0, middleware.GetProcessedCount())
}

func TestIdempotencyMiddleware_ClearKeepsActiveCommandLock(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	started := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)
	secondReturned := make(chan struct{})
	var executionCount int32

	firstNext := func(ctx context.Context, msg messaging.IMessage) error {
		atomic.AddInt32(&executionCount, 1)
		close(started)
		<-release
		return nil
	}
	secondNext := func(ctx context.Context, msg messaging.IMessage) error {
		atomic.AddInt32(&executionCount, 1)
		return nil
	}

	go func() {
		firstErr <- middleware.Handle(context.Background(), cmd, firstNext)
	}()

	<-started
	middleware.Clear()

	go func() {
		defer close(secondReturned)
		secondErr <- middleware.Handle(context.Background(), cmd, secondNext)
	}()

	select {
	case <-secondReturned:
		t.Fatal("expected Clear to keep active command lock until first execution finishes")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)

	select {
	case <-secondReturned:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second execution")
	}
	assert.NoError(t, <-firstErr)
	assert.NoError(t, <-secondErr)
	assert.Equal(t, int32(1), atomic.LoadInt32(&executionCount))
}

// TestIdempotencyMiddleware_Name 验证 IdempotencyMiddleware Name。
func TestIdempotencyMiddleware_Name(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	assert.Equal(t, "CommandIdempotency", middleware.Name())
}

func TestIdempotencyMiddleware_StopCanceledContextStillStops(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, middleware.Stop(ctx))

	select {
	case <-middleware.stopCleanup:
	default:
		t.Fatal("expected stop cleanup channel to be closed")
	}
}

// TestIdempotencyMiddleware_MaxProcessedEviction 验证 IdempotencyMiddleware MaxProcessedEviction。
func TestIdempotencyMiddleware_MaxProcessedEviction(t *testing.T) {
	mc := clock.NewManualClock(time.Now())
	config := &IdempotencyConfig{
		TTL:             time.Hour,
		CleanupInterval: time.Hour,
		MaxProcessed:    2,
		Clock:           mc,
	}
	middleware := NewIdempotencyMiddleware(config)
	defer func() { _ = middleware.Stop(context.Background()) }()

	next := func(ctx context.Context, msg messaging.IMessage) error { return nil }

	cmd1 := command.NewCommand("cmd-1", "A", "1", "X", nil)
	cmd2 := command.NewCommand("cmd-2", "A", "1", "X", nil)
	cmd3 := command.NewCommand("cmd-3", "A", "1", "X", nil)

	assert.NoError(t, middleware.Handle(context.Background(), cmd1, next))
	mc.Advance(time.Millisecond) // 确保 cmd1 processedAt < cmd2
	assert.NoError(t, middleware.Handle(context.Background(), cmd2, next))
	mc.Advance(time.Millisecond) // 确保 cmd2 processedAt < cmd3
	assert.NoError(t, middleware.Handle(context.Background(), cmd3, next))

	assert.Equal(t, 2, middleware.GetProcessedCount())
	assert.False(t, middleware.isProcessed("cmd-1"))
	assert.True(t, middleware.isProcessed("cmd-2"))
	assert.True(t, middleware.isProcessed("cmd-3"))
}
