package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"gochen/messaging"
	"gochen/messaging/command"
)

// mockValidator 模拟验证器
type mockValidator struct {
	shouldFail bool
}

func (v *mockValidator) Struct(s interface{}) error {
	if v.shouldFail {
		return errors.New("validation failed")
	}
	return nil
}

func (v *mockValidator) Var(field interface{}, tag string) error {
	return nil
}

// TestValidationMiddleware_Success 测试验证成功
func TestValidationMiddleware_Success(t *testing.T) {
	validator := &mockValidator{shouldFail: false}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	payload := map[string]interface{}{"name": "test"}
	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", payload)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
}

// TestValidationMiddleware_Failure 测试验证失败
func TestValidationMiddleware_Failure(t *testing.T) {
	validator := &mockValidator{shouldFail: true}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	payload := map[string]interface{}{"name": "test"}
	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", payload)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
	assert.False(t, nextCalled)
}

// TestValidationMiddleware_NilPayload 测试空 Payload
func TestValidationMiddleware_NilPayload(t *testing.T) {
	validator := &mockValidator{shouldFail: true}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", 1, "User", nil)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled) // 应该跳过验证
}

// TestValidationMiddleware_NonCommandMessage 测试非命令消息
func TestValidationMiddleware_NonCommandMessage(t *testing.T) {
	validator := &mockValidator{shouldFail: true}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	// 创建事件消息
	msg := &messaging.Message{
		ID:   "msg-1",
		Type: messaging.MessageTypeEvent,
	}

	err := middleware.Handle(context.Background(), msg, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled) // 应该透传
}

// TestValidationMiddleware_Name 测试中间件名称
func TestValidationMiddleware_Name(t *testing.T) {
	validator := &mockValidator{}
	middleware := NewValidationMiddleware(validator)

	assert.Equal(t, "CommandValidation", middleware.Name())
}
