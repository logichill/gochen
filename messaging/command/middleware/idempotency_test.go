package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/testkit/assert"

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

func TestIdempotencyMiddleware_UsesMetadataIdempotencyKey(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	var executionCount int
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	first := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	first.WithMetadata(command.MetadataIdempotencyKey, "request-1")
	second := command.NewCommand("cmd-2", "CreateUser", "1", "User", nil)
	second.WithMetadata(command.MetadataIdempotencyKey, " request-1 ")

	assert.NoError(t, middleware.Handle(context.Background(), first, next))
	assert.NoError(t, middleware.Handle(context.Background(), second, next))
	assert.Equal(t, 1, executionCount)
}

func TestIdempotencyMiddleware_BlankMetadataKeyFallsBackToCommandID(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	var executionCount int
	next := func(ctx context.Context, msg messaging.IMessage) error {
		executionCount++
		return nil
	}

	first := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	first.WithMetadata(command.MetadataIdempotencyKey, " ")
	second := command.NewCommand("cmd-2", "CreateUser", "1", "User", nil)
	second.WithMetadata(command.MetadataIdempotencyKey, " ")

	assert.NoError(t, middleware.Handle(context.Background(), first, next))
	assert.NoError(t, middleware.Handle(context.Background(), second, next))
	assert.Equal(t, 2, executionCount)
}

func TestIdempotencyMiddleware_PublishAllTransportFailureDoesNotMarkProcessed(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	transport := &publishAllFailsOnceTransport{}
	bus := messaging.NewMessageBus(transport)
	bus.Use(middleware)

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

	err := bus.PublishAll(context.Background(), []messaging.IMessage{cmd})
	assert.Error(t, err)
	assert.Equal(t, 0, middleware.GetProcessedCount())
	assert.Equal(t, 1, transport.publishAllCalls)

	assert.NoError(t, bus.PublishAll(context.Background(), []messaging.IMessage{cmd}))
	assert.Equal(t, 1, middleware.GetProcessedCount())
	assert.Equal(t, 2, transport.publishAllCalls)

	assert.NoError(t, bus.PublishAll(context.Background(), []messaging.IMessage{cmd}))
	assert.Equal(t, 1, middleware.GetProcessedCount())
	assert.Equal(t, 2, transport.publishAllCalls)
}

func TestIdempotencyMiddleware_PublishAllBatchTransportFailureDoesNotMarkProcessed(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()

	transport := &publishAllAlwaysFailsTransport{}
	bus := messaging.NewMessageBus(transport)
	bus.Use(middleware)

	cmd1 := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	cmd2 := command.NewCommand("cmd-2", "CreateUser", "2", "User", nil)
	messages := []messaging.IMessage{cmd1, cmd2}

	err := bus.PublishAll(context.Background(), messages)
	assert.Error(t, err)
	assert.Equal(t, 0, middleware.GetProcessedCount())
	assert.Equal(t, 1, transport.publishAllCalls)
	assert.Equal(t, []string{"cmd-1", "cmd-2"}, transport.batches[0])

	err = bus.PublishAll(context.Background(), messages)
	assert.Error(t, err)
	assert.Equal(t, 0, middleware.GetProcessedCount())
	assert.Equal(t, 2, transport.publishAllCalls)
	assert.Equal(t, []string{"cmd-1", "cmd-2"}, transport.batches[1])
}

type blockedBatchTransport struct {
	messaging.ITransport
	entered chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

func (t *blockedBatchTransport) PublishAll(context.Context, []messaging.IMessage) error {
	t.calls.Add(1)
	t.entered <- struct{}{}
	<-t.release
	return nil
}

func TestIdempotencyMiddleware_ConcurrentBatchesDoNotPublishDuplicates(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	transport := &blockedBatchTransport{entered: make(chan struct{}, 2), release: release}
	bus := messaging.NewMessageBus(transport)
	bus.Use(middleware)
	messages := func() []messaging.IMessage {
		return []messaging.IMessage{
			command.NewCommand("cmd-1", "CreateUser", "1", "User", nil),
			command.NewCommand("cmd-2", "CreateUser", "2", "User", nil),
		}
	}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- bus.PublishAll(context.Background(), messages()) }()
	<-transport.entered
	go func() { second <- bus.PublishAll(context.Background(), messages()) }()
	var secondErr error
	select {
	case secondErr = <-second:
		unblock()
	case <-transport.entered:
		unblock()
		secondErr = <-second
	}
	assert.NoError(t, <-first)
	if !errors.Is(secondErr, errors.Concurrency) {
		t.Fatalf("concurrent batch = %v, transport calls = %d", secondErr, transport.calls.Load())
	}
	assert.NoError(t, bus.PublishAll(context.Background(), messages()))
	assert.Equal(t, int32(1), transport.calls.Load())
	assert.Equal(t, 2, middleware.GetProcessedCount())
}

func TestIdempotencyMiddleware_BatchesWithOppositeOrderDoNotDeadlock(t *testing.T) {
	middleware := NewIdempotencyMiddleware(nil)
	defer func() { _ = middleware.Stop(context.Background()) }()
	first, finishFirst := middleware.BeginPublishAllBatch(context.Background())
	defer finishFirst(false)
	second, finishSecond := middleware.BeginPublishAllBatch(context.Background())
	defer finishSecond(false)
	a := command.NewCommand("a", "CreateUser", "1", "User", nil)
	b := command.NewCommand("b", "CreateUser", "2", "User", nil)
	next := func(context.Context, messaging.IMessage) error { return nil }
	assert.NoError(t, middleware.Handle(first, a, next))
	assert.NoError(t, middleware.Handle(second, b, next))
	done := make(chan error, 1)
	go func() { done <- middleware.Handle(first, b, next) }()
	select {
	case err := <-done:
		assert.True(t, errors.Is(err, errors.Concurrency))
	case <-time.After(time.Second):
		finishSecond(false)
		<-done
		t.Fatal("batch blocked while already holding a different command lock")
	}
	finishFirst(false)
	assert.NoError(t, middleware.Handle(second, a, next))
	finishSecond(true)
	assert.Equal(t, 2, middleware.GetProcessedCount())
}

func TestIdempotencyMiddleware_BatchScopesAreIndependent(t *testing.T) {
	first, second := NewIdempotencyMiddleware(nil), NewIdempotencyMiddleware(nil)
	defer func() { _ = first.Stop(context.Background()); _ = second.Stop(context.Background()) }()
	release := make(chan struct{})
	close(release)
	transport := &blockedBatchTransport{entered: make(chan struct{}, 1), release: release}
	bus := messaging.NewMessageBus(transport)
	bus.Use(first)
	bus.Use(second)
	err := bus.PublishAll(context.Background(), []messaging.IMessage{
		command.NewCommand("a", "CreateUser", "1", "User", nil),
		command.NewCommand("b", "CreateUser", "2", "User", nil),
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), transport.calls.Load())
	assert.Equal(t, 2, first.GetProcessedCount())
	assert.Equal(t, 2, second.GetProcessedCount())
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
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
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
	case <-time.After(time.Second):
		t.Fatal("expected contended command to fail without waiting")
	}
	assert.ErrorIs(t, <-secondErr, errors.Concurrency)
	unblock()
	assert.NoError(t, <-firstErr)
	assert.NoError(t, middleware.Handle(context.Background(), cmd, secondNext))
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

type publishAllFailsOnceTransport struct {
	publishAllCalls int
}

func (t *publishAllFailsOnceTransport) Publish(_ context.Context, _ messaging.IMessage) error {
	return errors.NewCode(errors.Internal, "unexpected Publish call")
}

func (t *publishAllFailsOnceTransport) PublishAll(_ context.Context, messages []messaging.IMessage) error {
	t.publishAllCalls++
	if len(messages) != 1 {
		return errors.NewCode(errors.Internal, "expected single-message PublishAll")
	}
	if t.publishAllCalls == 1 {
		return errors.NewCode(errors.Dependency, "transport publish all failed")
	}
	return nil
}

func (t *publishAllFailsOnceTransport) Subscribe(context.Context, string, messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return nil, errors.NewCode(errors.Internal, "unexpected Subscribe call")
}

func (t *publishAllFailsOnceTransport) Start(context.Context) error {
	return nil
}

func (t *publishAllFailsOnceTransport) Stop(context.Context) error {
	return nil
}

func (t *publishAllFailsOnceTransport) Stats() messaging.TransportStats {
	return messaging.TransportStats{}
}

type publishAllAlwaysFailsTransport struct {
	publishAllCalls int
	batches         [][]string
}

func (t *publishAllAlwaysFailsTransport) Publish(_ context.Context, _ messaging.IMessage) error {
	return errors.NewCode(errors.Internal, "unexpected Publish call")
}

func (t *publishAllAlwaysFailsTransport) PublishAll(_ context.Context, messages []messaging.IMessage) error {
	t.publishAllCalls++
	batch := make([]string, 0, len(messages))
	for _, message := range messages {
		batch = append(batch, message.GetID())
	}
	t.batches = append(t.batches, batch)
	return errors.NewCode(errors.Dependency, "transport publish all failed")
}

func (t *publishAllAlwaysFailsTransport) Subscribe(context.Context, string, messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	return nil, errors.NewCode(errors.Internal, "unexpected Subscribe call")
}

func (t *publishAllAlwaysFailsTransport) Start(context.Context) error {
	return nil
}

func (t *publishAllAlwaysFailsTransport) Stop(context.Context) error {
	return nil
}

func (t *publishAllAlwaysFailsTransport) Stats() messaging.TransportStats {
	return messaging.TransportStats{}
}
