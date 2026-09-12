package messaging_test

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/transport/memory"
	"gochen/testkit/require"
)

type batchMiddlewareFunc func(context.Context, messaging.IMessage, messaging.HandlerFunc) error

func (f batchMiddlewareFunc) Name() string { return "BatchTest" }
func (f batchMiddlewareFunc) Handle(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
	return f(ctx, msg, next)
}

func TestMessageBus_PublishAllRetryRollsBackFailedFanout(t *testing.T) {
	ctx := context.Background()
	transport := memory.NewMemoryTransportForTest(8)
	require.NoError(t, transport.Start(ctx))
	defer func() { _ = transport.Stop(ctx) }()
	bus := messaging.NewMessageBus(transport)
	bus.Use(batchMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
		if err := next(ctx, msg); err != nil {
			return next(ctx, msg)
		}
		return nil
	}))
	message := func(id string) messaging.IMessage {
		return messaging.NewMessage(id, messaging.KindEvent, "test", nil)
	}
	failed := false
	bus.Use(batchMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
		if msg.GetID() != "expand" {
			return next(ctx, msg)
		}
		prefix := "accepted"
		if !failed {
			prefix = "failed"
			failed = true
		}
		for _, suffix := range []string{"-1", "-2"} {
			if err := next(ctx, message(prefix+suffix)); err != nil {
				return err
			}
		}
		if prefix == "failed" {
			return errors.NewCode(errors.Dependency, "failed after next")
		}
		return nil
	}))
	require.NoError(t, bus.PublishAll(ctx, []messaging.IMessage{message("kept"), message("expand"), message("tail")}))
	pending, err := transport.StopWithSnapshot(ctx)
	require.NoError(t, err)
	ids := make([]string, len(pending))
	for i, msg := range pending {
		ids[i] = msg.GetID()
	}
	require.Equal(t, []string{"kept", "accepted-1", "accepted-2", "tail"}, ids)
}
