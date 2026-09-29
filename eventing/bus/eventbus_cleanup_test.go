package bus

import (
	"context"
	stderrors "errors"
	"testing"

	"gochen/messaging"
	"gochen/testkit/require"
)

// cleanupSubscriber 独立提供可重试订阅，避免 MessageBus 掩盖 EventBus 的状态错误。
type cleanupSubscriber struct {
	messaging.IMessageBus
	failures     map[string]error
	active       map[string]bool
	calls        []string
	subscribeErr error
}

func (s *cleanupSubscriber) Subscribe(_ context.Context, kind string, _ messaging.IMessageHandler) (messaging.UnsubscribeFunc, error) {
	if kind == "fail" {
		return nil, s.subscribeErr
	}
	s.active[kind] = true
	return func(context.Context) error {
		s.calls = append(s.calls, kind)
		if err := s.failures[kind]; err != nil {
			delete(s.failures, kind)
			return err
		}
		if !s.active[kind] {
			return stderrors.New("successful subscription released twice")
		}
		delete(s.active, kind)
		return nil
	}, nil
}

type cleanupEventHandler struct {
	EventHandlerFunc
	kinds []string
}

func (h cleanupEventHandler) EventTypes() []string { return h.kinds }

func TestEventBus_UnsubscribeRetriesOnlyFailuresAndJoinsErrors(t *testing.T) {
	ctx := context.Background()
	aErr, cErr := stderrors.New("A failed"), stderrors.New("C failed")
	subscriber := &cleanupSubscriber{
		failures: map[string]error{"A": aErr, "C": cErr},
		active:   make(map[string]bool),
	}
	eventBus := NewEventBus(subscriber)
	release, err := eventBus.SubscribeHandler(ctx, cleanupEventHandler{kinds: []string{"A", "B", "C"}})
	require.NoError(t, err)
	err = release(ctx)
	require.True(t, stderrors.Is(err, aErr))
	require.True(t, stderrors.Is(err, cErr))
	require.Equal(t, []string{"C", "B", "A"}, subscriber.calls)
	require.Equal(t, 2, len(subscriber.active))
	require.NoError(t, release(ctx))
	require.Equal(t, []string{"C", "B", "A", "C", "A"}, subscriber.calls)
	require.Equal(t, 0, len(subscriber.active))
	require.NoError(t, release(ctx))
	require.Equal(t, 5, len(subscriber.calls))
}

func TestEventBus_FailedRegistrationReturnsPendingCleanup(t *testing.T) {
	ctx := context.Background()
	subscribeErr, rollbackErr := stderrors.New("subscribe failed"), stderrors.New("rollback failed")
	for _, failRollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete rollback", true: "pending rollback"}[failRollback], func(t *testing.T) {
			subscriber := &cleanupSubscriber{
				failures:     make(map[string]error),
				active:       make(map[string]bool),
				subscribeErr: subscribeErr,
			}
			if failRollback {
				subscriber.failures["A"] = rollbackErr
			}
			release, err := NewEventBus(subscriber).SubscribeHandler(ctx, cleanupEventHandler{kinds: []string{"A", "B", "fail"}})
			require.True(t, stderrors.Is(err, subscribeErr))
			require.Equal(t, []string{"B", "A"}, subscriber.calls)
			if failRollback {
				require.True(t, stderrors.Is(err, rollbackErr))
				require.NotNil(t, release)
				require.Equal(t, 1, len(subscriber.active))
				require.NoError(t, release(ctx))
				require.NoError(t, release(ctx))
				require.Equal(t, []string{"B", "A", "A"}, subscriber.calls)
			} else {
				require.Nil(t, release)
			}
			require.Equal(t, 0, len(subscriber.active))
		})
	}
}
