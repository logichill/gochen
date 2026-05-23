package command

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/messaging"
	synctransport "gochen/messaging/transport/direct"
	"gochen/messaging/transport/memory"
)

type testMiddleware struct {
	beforeFunc func(ctx context.Context, msg messaging.IMessage)
}

// Handle 处理消息并执行业务处理逻辑。
func (m *testMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	if m.beforeFunc != nil {
		m.beforeFunc(ctx, message)
	}
	return next(ctx, message)
}

// Name 返回名称。
func (m *testMiddleware) Name() string { return "test" }

func TestCommandBus_Dispatch(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer func() { _ = transport.Stop(context.Background()) }()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus)

	handlerCalled := false
	unsub, err := messageBus.Subscribe(context.Background(), "CreateUser", CommandHandlerFunc(func(ctx context.Context, cmd *Command) error {
		handlerCalled = true
		assert.Equal(t, "CreateUser", cmd.GetCommandType())
		return nil
	}).AsMessageHandler("CreateUser"))
	require.NoError(t, err)
	defer func() { _ = unsub(context.Background()) }()

	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", map[string]any{"name": "test"})
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestCommandBus_Middleware(t *testing.T) {
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(context.Background()))
	defer func() { _ = transport.Stop(context.Background()) }()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus)

	var middlewareExecuted bool
	commandBus.Use(&testMiddleware{beforeFunc: func(ctx context.Context, msg messaging.IMessage) {
		middlewareExecuted = true
	}})

	var handlerExecuted bool
	unsub, err := messageBus.Subscribe(context.Background(), "TestCommand", CommandHandlerFunc(func(ctx context.Context, cmd *Command) error {
		handlerExecuted = true
		return nil
	}).AsMessageHandler("TestCommand"))
	require.NoError(t, err)
	defer func() { _ = unsub(context.Background()) }()

	cmd := NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	err = commandBus.Dispatch(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, middlewareExecuted)
	assert.True(t, handlerExecuted)
}

func TestCommandBus_ConcurrentDispatch(t *testing.T) {
	transport := memory.NewMemoryTransport(100, 4)
	require.NoError(t, transport.Start(context.Background()))
	defer func() { _ = transport.Stop(context.Background()) }()

	messageBus := messaging.NewMessageBus(transport)
	commandBus := NewCommandBus(messageBus)

	var counter int64
	var processed sync.WaitGroup
	numCommands := 100
	processed.Add(numCommands)

	unsub, err := messageBus.Subscribe(context.Background(), "TestCommand", CommandHandlerFunc(func(ctx context.Context, cmd *Command) error {
		defer processed.Done()
		atomic.AddInt64(&counter, 1)
		return nil
	}).AsMessageHandler("TestCommand"))
	require.NoError(t, err)
	defer func() { _ = unsub(context.Background()) }()

	var wg sync.WaitGroup
	for i := 0; i < numCommands; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cmd := NewCommand(fmt.Sprintf("cmd-%d", id), "TestCommand", fmt.Sprintf("%d", id), "Test", nil)
			_ = commandBus.Dispatch(context.Background(), cmd)
		}(i)
	}

	wg.Wait()
	processed.Wait()

	assert.Equal(t, int64(numCommands), atomic.LoadInt64(&counter))
}

func TestCommandBus_NilCommand(t *testing.T) {
	commandBus := NewCommandBus(nil)
	err := commandBus.Dispatch(context.Background(), nil)
	require.Error(t, err)
}

func TestCommandExecutor_RegisterAndExecute(t *testing.T) {
	executor := NewCommandExecutor()

	handlerCalled := false
	require.NoError(t, executor.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		handlerCalled = true
		assert.Equal(t, "CreateUser", cmd.GetCommandType())
		return nil
	}))

	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", map[string]any{"name": "test"})
	err := executor.Execute(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestCommandExecutor_HandlerError(t *testing.T) {
	executor := NewCommandExecutor()
	require.NoError(t, executor.RegisterHandler("FailCommand", func(ctx context.Context, cmd *Command) error {
		return fmt.Errorf("handler error")
	}))

	cmd := NewCommand("cmd-1", "FailCommand", "1", "Test", nil)
	err := executor.Execute(context.Background(), cmd)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "handler error")
}

func TestCommandExecutor_Middleware(t *testing.T) {
	executor := NewCommandExecutor()

	var middlewareExecuted bool
	executor.Use(&testMiddleware{beforeFunc: func(ctx context.Context, msg messaging.IMessage) {
		middlewareExecuted = true
	}})

	var handlerExecuted bool
	require.NoError(t, executor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *Command) error {
		handlerExecuted = true
		return nil
	}))

	cmd := NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	err := executor.Execute(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, middlewareExecuted)
	assert.True(t, handlerExecuted)
}

func TestCommandExecutor_RegisterHandler_ReplacesExisting(t *testing.T) {
	executor := NewCommandExecutor()

	var firstCalled int32
	require.NoError(t, executor.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&firstCalled, 1)
		return nil
	}))

	var secondCalled int32
	require.NoError(t, executor.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		atomic.AddInt32(&secondCalled, 1)
		return nil
	}))

	cmd := NewCommand("cmd-1", "CreateUser", "1", "User", nil)
	require.NoError(t, executor.Execute(context.Background(), cmd))

	assert.Equal(t, int32(0), atomic.LoadInt32(&firstCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&secondCalled))
}

func TestCommandExecutor_HasHandler(t *testing.T) {
	executor := NewCommandExecutor()
	require.NoError(t, executor.RegisterHandler("CreateUser", func(ctx context.Context, cmd *Command) error {
		return nil
	}))

	assert.True(t, executor.HasHandler("CreateUser"))
	assert.False(t, executor.HasHandler("DeleteUser"))
}

func TestCommandExecutor_NilCommand(t *testing.T) {
	executor := NewCommandExecutor()
	err := executor.Execute(context.Background(), nil)
	require.Error(t, err)
}

func TestCommandExecutor_NoHandler(t *testing.T) {
	executor := NewCommandExecutor()
	err := executor.Execute(context.Background(), NewCommand("cmd-1", "MissingCommand", "1", "Test", nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.NotFound))
}
