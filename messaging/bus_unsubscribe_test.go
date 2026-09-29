package messaging_test

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/transport/direct"
	"gochen/testkit/require"
)

type blockingUnsubscribeTransport struct {
	*direct.SyncTransport
	entered  chan struct{}
	unblock  chan struct{}
	attempts atomic.Int32
	failure  error
}

func (t *blockingUnsubscribeTransport) Subscribe(ctx context.Context, kind string, handler messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	unsub, err := t.SyncTransport.Subscribe(ctx, kind, handler)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		if t.attempts.Add(1) == 1 {
			close(t.entered)
			<-t.unblock
			return t.failure
		}
		return unsub(ctx)
	}, nil
}

func TestMessageBus_UnsubscribeRetriesFailureAndHonorsConcurrentDeadline(t *testing.T) {
	ctx := context.Background()
	transport := &blockingUnsubscribeTransport{
		SyncTransport: direct.NewSyncTransport(),
		entered:       make(chan struct{}),
		unblock:       make(chan struct{}),
		failure:       stderrors.New("temporary unsubscribe failure"),
	}
	bus := messaging.NewMessageBus(transport)
	unsub, err := bus.Subscribe(ctx, "event", &recordingHandler{})
	require.NoError(t, err)
	unblock := sync.OnceFunc(func() { close(transport.unblock) })
	firstDone := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		firstDone <- unsub(ctx)
	}()
	t.Cleanup(func() {
		unblock()
		<-finished
	})
	select {
	case <-transport.entered:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not start")
	}

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	waitDone := make(chan error, 1)
	go func() { waitDone <- unsub(waitCtx) }()
	select {
	case err := <-waitDone:
		require.True(t, stderrors.Is(err, context.DeadlineExceeded))
	case <-time.After(time.Second):
		t.Fatal("concurrent unsubscribe ignored its deadline")
	}
	require.Equal(t, int32(1), transport.attempts.Load())
	unblock()
	require.True(t, stderrors.Is(<-firstDone, transport.failure))
	require.Equal(t, 1, transport.Stats().HandlerCount)

	const callers = 12
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { results <- unsub(ctx) }()
	}
	for i := 0; i < callers; i++ {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(2), transport.attempts.Load())
	require.Equal(t, 0, transport.Stats().HandlerCount)
	require.NoError(t, unsub(waitCtx))
	require.True(t, errors.Is(unsub(nil), errors.InvalidInput))
}
