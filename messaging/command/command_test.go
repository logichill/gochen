package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gochen/messaging"
)

// TestNewCommand 测试命令创建
func TestNewCommand(t *testing.T) {
	payload := map[string]any{"name": "test"}
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", payload)

	assert.Equal(t, "cmd-1", cmd.GetID())
	assert.Equal(t, messaging.MessageTypeCommand, cmd.GetType())
	assert.Equal(t, int64(123), cmd.GetAggregateID())
	assert.Equal(t, "User", cmd.GetAggregateType())
	assert.Equal(t, payload, cmd.GetPayload())
	assert.NotNil(t, cmd.GetTimestamp())
	assert.NotNil(t, cmd.GetMetadata())

	// 验证元数据
	assert.Equal(t, int64(123), cmd.GetMetadata()["aggregate_id"])
	assert.Equal(t, "User", cmd.GetMetadata()["aggregate_type"])
	assert.Equal(t, "CreateUser", cmd.GetMetadata()["command_type"])
}

// TestCommand_GetCommandType 测试获取命令类型
func TestCommand_GetCommandType(t *testing.T) {
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", nil)
	assert.Equal(t, "CreateUser", cmd.GetCommandType())
}

// TestCommand_ChainedMethods 测试链式方法
func TestCommand_ChainedMethods(t *testing.T) {
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", nil).
		WithUserID(456).
		WithRequestID("req-1").
		WithCorrelationID("corr-1").
		WithCausationID("cause-1").
		WithMetadata("custom", "value")

	assert.Equal(t, int64(456), cmd.UserID)
	assert.Equal(t, "req-1", cmd.RequestID)
	assert.Equal(t, int64(456), cmd.GetMetadata()["user_id"])
	assert.Equal(t, "req-1", cmd.GetMetadata()["request_id"])
	assert.Equal(t, "corr-1", cmd.GetMetadata()["correlation_id"])
	assert.Equal(t, "cause-1", cmd.GetMetadata()["causation_id"])
	assert.Equal(t, "value", cmd.GetMetadata()["custom"])
}

// TestCommand_ImplementsIMessage 测试 Command 实现 IMessage 接口
func TestCommand_ImplementsIMessage(t *testing.T) {
	cmd := NewCommand("cmd-1", "CreateUser", 123, "User", nil)

	// 确保可以赋值给 IMessage 接口
	var msg messaging.IMessage = cmd
	assert.NotNil(t, msg)

	// 验证接口方法
	assert.Equal(t, "cmd-1", msg.GetID())
	assert.Equal(t, messaging.MessageTypeCommand, msg.GetType())
	assert.NotZero(t, msg.GetTimestamp())
	assert.NotNil(t, msg.GetMetadata())
}

// TestCommandHandlerFunc_AsMessageHandler 测试处理器适配
func TestCommandHandlerFunc_AsMessageHandler(t *testing.T) {
	executed := false
	handler := CommandHandlerFunc(func(ctx context.Context, cmd *Command) error {
		executed = true
		assert.Equal(t, "cmd-1", cmd.GetID())
		return nil
	})

	// 转换为 IMessageHandler
	msgHandler := handler.AsMessageHandler("TestHandler")
	assert.Equal(t, "TestHandler", msgHandler.Type())

	// 执行处理
	cmd := NewCommand("cmd-1", "TestCommand", 1, "Test", nil)
	err := msgHandler.Handle(context.Background(), cmd)

	assert.NoError(t, err)
	assert.True(t, executed)
}

// TestCommandHandlerFunc_InvalidMessageType 测试无效消息类型
func TestCommandHandlerFunc_InvalidMessageType(t *testing.T) {
	handler := CommandHandlerFunc(func(ctx context.Context, cmd *Command) error {
		t.Fatal("should not be called")
		return nil
	})

	msgHandler := handler.AsMessageHandler("TestHandler")

	// 传入非 Command 类型的消息
	regularMsg := &messaging.Message{
		ID:   "msg-1",
		Type: "event",
	}

	err := msgHandler.Handle(context.Background(), regularMsg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected *Command")
}

// TestTypedCommandHandler 测试泛型处理器
func TestTypedCommandHandler(t *testing.T) {
	type CreateUserPayload struct {
		Name  string
		Email string
	}

	executed := false
	handler := NewTypedCommandHandler[CreateUserPayload]("CreateUser",
		func(ctx context.Context, cmd *Command, payload CreateUserPayload) error {
			executed = true
			assert.Equal(t, "John", payload.Name)
			assert.Equal(t, "john@example.com", payload.Email)
			return nil
		})

	// 创建带 payload 的命令
	payload := CreateUserPayload{Name: "John", Email: "john@example.com"}
	cmd := NewCommand("cmd-1", "CreateUser", 1, "User", payload)

	// 执行
	err := handler.Handle(context.Background(), cmd)
	assert.NoError(t, err)
	assert.True(t, executed)
}

// TestTypedCommandHandler_InvalidPayloadType 测试无效 Payload 类型
func TestTypedCommandHandler_InvalidPayloadType(t *testing.T) {
	type CreateUserPayload struct {
		Name string
	}

	handler := NewTypedCommandHandler[CreateUserPayload]("CreateUser",
		func(ctx context.Context, cmd *Command, payload CreateUserPayload) error {
			t.Fatal("should not be called")
			return nil
		})

	// 传入错误类型的 payload
	cmd := NewCommand("cmd-1", "CreateUser", 1, "User", "wrong type")

	err := handler.Handle(context.Background(), cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payload type")
}

// BenchmarkCommand_Creation 命令创建性能测试
func BenchmarkCommand_Creation(b *testing.B) {
	payload := map[string]any{"name": "test"}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewCommand("cmd-1", "CreateUser", 123, "User", payload)
	}
}

// BenchmarkCommand_WithChaining 链式调用性能测试
func BenchmarkCommand_WithChaining(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewCommand("cmd-1", "CreateUser", 123, "User", nil).
			WithUserID(456).
			WithRequestID("req-1").
			WithCorrelationID("corr-1")
	}
}
