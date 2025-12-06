package command

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/messaging"
	"gochen/messaging/transport/memory"
	synctransport "gochen/messaging/transport/sync"
)

// TestCommandBus_RegisterAndDispatch 测试注册和分发
func TestCommandBus_RegisterAndDispatch(t *testing.T) {
	// 使用同步传输层，方便验证执行与错误传播
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 注册处理器
	executed := false
	err := commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		executed = true
		assert.Equal(t, "CreateUser", cmd.GetCommandType())
		assert.Equal(t, int64(123), cmd.GetAggregateID())
		return nil
	})
	require.NoError(t, err)

	// 分发命令
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", map[string]any{"name": "test"})
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, executed)
}

// TestCommandBus_HandlerError 测试处理器错误
func TestCommandBus_HandlerError(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 注册返回错误的处理器
	expectedErr := errors.New("handler error")
	var handlerCalled bool
	err := commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		handlerCalled = true
		return expectedErr
	})
	require.NoError(t, err)

	// 分发命令
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", nil)
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.Error(t, err)
	assert.True(t, handlerCalled)
}

// TestCommandBus_ConcurrentDispatch 测试并发分发不同聚合
func TestCommandBus_ConcurrentDispatch(t *testing.T) {
	transport := memory.NewMemoryTransport(100, 4)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 注册处理器
	var counter int64
	var processed sync.WaitGroup
	numCommands := 100
	processed.Add(numCommands)

	err := commandBus.RegisterHandler("TestCommand", func(ctx context.Context, cmd *Command) error {
		defer processed.Done()
		atomic.AddInt64(&counter, 1)
		return nil
	})
	require.NoError(t, err)

	// 并发分发不同聚合的命令（只保证发布完成，具体处理由 worker 异步完成）
	var wg sync.WaitGroup
	for i := 0; i < numCommands; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cmd := NewCommand("cmd-"+string(rune(id)), "TestCommand", int64(id), "Test", nil)
			_ = commandBus.Dispatch(context.Background(), cmd)
		}(i)
	}

	wg.Wait()
	processed.Wait()

	assert.Equal(t, int64(numCommands), atomic.LoadInt64(&counter))
}

// TestCommandBus_Middleware 测试中间件集成
func TestCommandBus_Middleware(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 添加测试中间件
	var middlewareExecuted bool
	testMiddleware := &testMiddleware{
		beforeFunc: func(ctx context.Context, msg messaging.IMessage) {
			middlewareExecuted = true
		},
	}
	commandBus.Use(testMiddleware)

	// 注册处理器
	var handlerExecuted bool
	err := commandBus.RegisterHandler("TestCommand", func(ctx context.Context, cmd *Command) error {
		handlerExecuted = true
		return nil
	})
	require.NoError(t, err)

	// 分发命令
	cmd := NewCommand("cmd-1", "TestCommand", 1, "Test", nil)
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, middlewareExecuted, "middleware should be executed")
	assert.True(t, handlerExecuted, "handler should be executed")
}

// TestCommandBus_RegisterHandler_ReplacesExisting 确保重复注册会替换旧处理器而非重复订阅
func TestCommandBus_RegisterHandler_ReplacesExisting(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	var firstCalled int32
	require.NoError(t, commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&firstCalled, 1)
		return nil
	}))

	var secondCalled int32
	require.NoError(t, commandBus.RegisterHandlerWithContext(context.Background(), "CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&secondCalled, 1)
		return nil
	}))

	cmd := NewCommand("cmd-1", "CreateUser", 1, "User", nil)
	require.NoError(t, commandBus.Dispatch(context.Background(), cmd))

	assert.Equal(t, int32(0), atomic.LoadInt32(&firstCalled), "旧处理器不应再被调用")
	assert.Equal(t, int32(1), atomic.LoadInt32(&secondCalled), "新处理器应被调用一次")
	assert.Equal(t, 1, transport.Stats().HandlerCount, "重复注册后应只保留一个订阅")
}

// TestCommandBus_HasHandler 测试处理器检查
func TestCommandBus_HasHandler(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 注册处理器
	err := commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		return nil
	})
	require.NoError(t, err)

	// 检查
	assert.True(t, commandBus.HasHandler("CreateUser"))
	assert.False(t, commandBus.HasHandler("DeleteUser"))
}

// Test command handler duplicate registration should replace existing handler
func TestCommandBus_RegisterHandler_Duplicate(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	var first int32
	require.NoError(t, commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&first, 1)
		return nil
	}))

	var second int32
	err := commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&second, 1)
		return nil
	})
	require.NoError(t, err)

	cmd := NewCommand("cmd-duplicate", "CreateUser", 1, "User", nil)
	require.NoError(t, commandBus.Dispatch(context.Background(), cmd))

	assert.Equal(t, int32(0), atomic.LoadInt32(&first))
	assert.Equal(t, int32(1), atomic.LoadInt32(&second))
}

// TestCommandBus_NilCommand 测试空命令
func TestCommandBus_NilCommand(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	err := commandBus.Dispatch(context.Background(), nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCommand())
}

// testMiddleware 测试用中间件
type testMiddleware struct {
	beforeFunc func(ctx context.Context, msg messaging.IMessage)
}

func (m *testMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	if m.beforeFunc != nil {
		m.beforeFunc(ctx, message)
	}
	return next(ctx, message)
}

func (m *testMiddleware) Name() string {
	return "TestMiddleware"
}

// BenchmarkCommandBus_Dispatch 命令分发性能测试
func BenchmarkCommandBus_Dispatch(b *testing.B) {
	transport := memory.NewMemoryTransport(10, 2)
	transport.Start(context.Background())
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	commandBus.RegisterHandler("BenchCommand", func(ctx context.Context, cmd *Command) error {
		return nil
	})

	cmd := NewCommand("cmd-1", "BenchCommand", 1, "Test", nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = commandBus.Dispatch(ctx, cmd)
	}
}

// BenchmarkCommandBus_ConcurrentDispatch 并发分发性能测试
func BenchmarkCommandBus_ConcurrentDispatch(b *testing.B) {
	transport := memory.NewMemoryTransport(100, 4)
	transport.Start(context.Background())
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	commandBus.RegisterHandler("BenchCommand", func(ctx context.Context, cmd *Command) error {
		return nil
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		i := 0
		for pb.Next() {
			cmd := NewCommand("cmd-"+string(rune(i)), "BenchCommand", int64(i), "Test", nil)
			_ = commandBus.Dispatch(ctx, cmd)
			i++
		}
	})
}
