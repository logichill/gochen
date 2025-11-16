package command

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/messaging"
	"gochen/messaging/transport/memory"
)

// TestCommandBus_RegisterAndDispatch 测试注册和分发
func TestCommandBus_RegisterAndDispatch(t *testing.T) {
	// 创建 CommandBus
	transport := memory.NewMemoryTransport(10, 2) // bufferSize=10, workerCount=2
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
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", map[string]interface{}{"name": "test"})
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, executed)
}

// TestCommandBus_HandlerError 测试处理器错误
func TestCommandBus_HandlerError(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	// 注册返回错误的处理器
	expectedErr := errors.New("handler error")
	err := commandBus.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		return expectedErr
	})
	require.NoError(t, err)

	// 分发命令
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", nil)
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

// TestCommandBus_AggregateLock 测试聚合级锁
func TestCommandBus_AggregateLock(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)

	// 启用聚合锁
	config := &CommandBusConfig{
		EnableAggregateLock: true,
	}
	commandBus := NewCommandBus(messageBus, config)

	// 注册慢速处理器
	var executionOrder []int
	var mu sync.Mutex

	err := commandBus.RegisterHandler("SlowCommand", func(ctx context.Context, cmd *Command) error {
		mu.Lock()
		order := len(executionOrder) + 1
		executionOrder = append(executionOrder, order)
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	// 并发分发相同聚合的命令
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := NewCommand("cmd-"+string(rune(i)), "SlowCommand", 123, "Test", nil)
			_ = commandBus.Dispatch(context.Background(), cmd)
		}()
	}

	wg.Wait()

	// 验证串行执行
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, len(executionOrder))
	assert.Equal(t, []int{1, 2, 3}, executionOrder)
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
	err := commandBus.RegisterHandler("TestCommand", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt64(&counter, 1)
		return nil
	})
	require.NoError(t, err)

	// 并发分发不同聚合的命令
	numCommands := 100
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

	assert.Equal(t, int64(numCommands), atomic.LoadInt64(&counter))
}

// TestCommandBus_Middleware 测试中间件集成
func TestCommandBus_Middleware(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
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

// TestCommandBus_NilCommand 测试空命令
func TestCommandBus_NilCommand(t *testing.T) {
	transport := memory.NewMemoryTransport(10, 2)
	require.NoError(t, transport.Start(context.Background()))
	defer transport.Close()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus, nil)

	err := commandBus.Dispatch(context.Background(), nil)
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidCommand, err)
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
