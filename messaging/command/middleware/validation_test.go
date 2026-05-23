package middleware

import (
	"context"
	"gochen/errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"gochen/messaging"
	"gochen/messaging/command"
)

// mockValidator 模拟验证器
type mockValidator struct {
	shouldFail bool
}

// Validate 校验对象（测试 stub，可配置是否失败）。
func (v *mockValidator) Validate(value any) error {
	_ = value
	if v.shouldFail {
		return errors.New("validation failed")
	}
	return nil
}

// TestValidationMiddleware_Success 验证 ValidationMiddleware Success。
func TestValidationMiddleware_Success(t *testing.T) {
	validator := &mockValidator{shouldFail: false}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	payload := map[string]any{"name": "test"}
	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", payload)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled)
}

// TestValidationMiddleware_Failure 验证 ValidationMiddleware Failure。
func TestValidationMiddleware_Failure(t *testing.T) {
	validator := &mockValidator{shouldFail: true}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	payload := map[string]any{"name": "test"}
	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", payload)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
	assert.False(t, nextCalled)
}

// TestValidationMiddleware_NilPayload 验证 ValidationMiddleware NilPayload。
func TestValidationMiddleware_NilPayload(t *testing.T) {
	validator := &mockValidator{shouldFail: true}
	middleware := NewValidationMiddleware(validator)

	nextCalled := false
	next := func(ctx context.Context, msg messaging.IMessage) error {
		nextCalled = true
		return nil
	}

	cmd := command.NewCommand("cmd-1", "CreateUser", "1", "User", nil)

	err := middleware.Handle(context.Background(), cmd, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled) // 应该跳过验证
}

// TestValidationMiddleware_NonCommandMessage 验证 ValidationMiddleware NonCommandMessage。
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
		Kind: messaging.KindEvent,
		Type: "TestEvent",
	}

	err := middleware.Handle(context.Background(), msg, next)

	assert.NoError(t, err)
	assert.True(t, nextCalled) // 应该透传
}

// TestValidationMiddleware_Name 验证 ValidationMiddleware Name。
func TestValidationMiddleware_Name(t *testing.T) {
	validator := &mockValidator{}
	middleware := NewValidationMiddleware(validator)

	assert.Equal(t, "CommandValidation", middleware.Name())
}
