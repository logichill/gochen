package bus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/messaging"
)

// testPayload 测试用 Payload 类型
type testPayload struct {
	Name  string
	Value int
}

func TestTypedEventHandler_Handle(t *testing.T) {
	var capturedPayload *testPayload
	var capturedEvent eventing.IEvent

	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created", "user.updated"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			capturedPayload = payload
			capturedEvent = evt
			return nil
		},
	)

	// 创建测试事件
	payload := &testPayload{Name: "test", Value: 42}
	evt := eventing.NewEvent[int64](1, "User", "user.created", 1, payload)

	// 执行处理
	err := handler.Handle(context.Background(), evt)
	require.NoError(t, err)

	// 验证
	assert.Equal(t, payload, capturedPayload)
	assert.Equal(t, evt, capturedEvent)
}

func TestTypedEventHandler_HandleEvent(t *testing.T) {
	var called bool

	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			called = true
			return nil
		},
	)

	payload := &testPayload{Name: "test", Value: 42}
	evt := eventing.NewEvent[int64](1, "User", "user.created", 1, payload)

	err := handler.HandleEvent(context.Background(), evt)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestTypedEventHandler_InvalidMessageType(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	// 传入非事件消息
	msg := messaging.NewMessage("1", messaging.KindCommand, "test.command", nil)
	err := handler.Handle(context.Background(), msg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an event")
}

func TestTypedEventHandler_InvalidPayloadType(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	// 创建带错误 Payload 类型的事件
	evt := eventing.NewEvent[int64](1, "User", "user.created", 1, "wrong-type")
	err := handler.HandleEvent(context.Background(), evt)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payload type")
}

func TestTypedEventHandler_HandlerError(t *testing.T) {
	expectedErr := errors.New("handler error")

	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return expectedErr
		},
	)

	payload := &testPayload{Name: "test", Value: 42}
	evt := eventing.NewEvent[int64](1, "User", "user.created", 1, payload)

	err := handler.HandleEvent(context.Background(), evt)
	assert.ErrorIs(t, err, expectedErr)
}

func TestTypedEventHandler_EventTypes(t *testing.T) {
	eventTypes := []string{"user.created", "user.updated", "user.deleted"}

	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		eventTypes,
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	assert.Equal(t, eventTypes, handler.EventTypes())
}

func TestTypedEventHandler_DefaultEventTypes(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		nil, // 空事件类型
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	assert.Equal(t, []string{"*"}, handler.EventTypes())
}

func TestTypedEventHandler_HandlerName(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"my-custom-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	assert.Equal(t, "my-custom-handler", handler.HandlerName())
}

func TestTypedEventHandler_Type(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"my-custom-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	assert.Equal(t, "my-custom-handler", handler.Type())
}

func TestTypedEventHandler_ImplementsIEventHandler(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload](
		"test-handler",
		[]string{"user.created"},
		func(ctx context.Context, evt eventing.IEvent, payload *testPayload) error {
			return nil
		},
	)

	// 编译期已验证，这里再做运行时验证
	var _ IEventHandler = handler
}

func TestTypedEventHandler_ValuePayload(t *testing.T) {
	// 测试值类型 Payload（非指针）
	type valuePayload struct {
		Data string
	}

	var capturedPayload valuePayload

	handler := NewTypedEventHandler[valuePayload](
		"value-handler",
		[]string{"test.event"},
		func(ctx context.Context, evt eventing.IEvent, payload valuePayload) error {
			capturedPayload = payload
			return nil
		},
	)

	payload := valuePayload{Data: "test-data"}
	evt := eventing.NewEvent[int64](1, "Test", "test.event", 1, payload)

	err := handler.HandleEvent(context.Background(), evt)
	require.NoError(t, err)
	assert.Equal(t, payload, capturedPayload)
}

func TestTypedEventHandler_NilFunc_DoesNotPanic(t *testing.T) {
	handler := NewTypedEventHandler[*testPayload]("nil-fn", []string{"test.event"}, nil)

	payload := &testPayload{Name: "test", Value: 42}
	evt := eventing.NewEvent[int64](1, "Test", "test.event", 1, payload)

	require.NotPanics(t, func() {
		err := handler.HandleEvent(context.Background(), evt)
		require.Error(t, err)
		assert.True(t, errors.Is(err, errors.InvalidInput))
	})
}
