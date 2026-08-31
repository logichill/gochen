package subscription

import (
	"context"
	"gochen/eventing/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// flakyStreamStore 包装内存事件存储，在前 N 次 StreamEvents 调用注入瞬时错误。
type flakyStreamStore struct {
	*store.MemoryEventStore[int64]
	failuresLeft atomic.Int32
	calls        atomic.Int32
}

func (f *flakyStreamStore) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	f.calls.Add(1)
	if f.failuresLeft.Load() > 0 {
		f.failuresLeft.Add(-1)
		return nil, errors.NewCode(errors.Database, "transient stream error")
	}
	return f.MemoryEventStore.StreamEvents(ctx, opts)
}

// TestSubscription_Run_RecoversFromTransientPollError 验证瞬时轮询错误退避后恢复消费。
func TestSubscription_Run_RecoversFromTransientPollError(t *testing.T) {
	flaky := &flakyStreamStore{MemoryEventStore: store.NewMemoryEventStore[int64]()}
	flaky.failuresLeft.Store(3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var delivered atomic.Int32
	sub, err := New[int64](flaky, func(ctx context.Context, evt eventing.Event[int64]) error {
		if delivered.Add(1) == 1 {
			cancel()
		}
		return nil
	}, &Config{
		PollInterval:     time.Millisecond,
		PollRetryBackoff: time.Millisecond,
		MaxPollRetries:   5,
		BatchSize:        10,
		FromTime:         time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, map[string]any{"x": 1})
	if err := flaky.AppendEvents(context.Background(), "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	_ = sub.Run(ctx)

	if delivered.Load() == 0 {
		t.Fatalf("expected event delivered after transient poll errors recovered")
	}
}

// TestSubscription_Run_FailsFastAfterSustainedPollErrors 验证持久轮询错误超过上限后 Run 返回错误。
func TestSubscription_Run_FailsFastAfterSustainedPollErrors(t *testing.T) {
	flaky := &flakyStreamStore{MemoryEventStore: store.NewMemoryEventStore[int64]()}
	flaky.failuresLeft.Store(1 << 30) // 始终失败

	sub, err := New[int64](flaky, func(ctx context.Context, evt eventing.Event[int64]) error {
		return nil
	}, &Config{
		PollInterval:     time.Millisecond,
		PollRetryBackoff: time.Millisecond,
		MaxPollRetries:   3,
		BatchSize:        10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	runErr := sub.Run(context.Background())
	if runErr == nil {
		t.Fatalf("expected Run to return error after sustained poll failures")
	}
	// MaxPollRetries=3：第 4 次失败触发返回，因此恰好调用 4 次。
	if got := flaky.calls.Load(); got != 4 {
		t.Fatalf("expected 4 poll attempts, got %d", got)
	}
}

func TestSubscription_PollBackoffCapsWithoutOverflow(t *testing.T) {
	sub := &Subscription[int64]{cfg: &Config{
		PollInterval:     time.Duration(1<<63 - 1),
		PollRetryBackoff: time.Duration(1<<63 - 1),
	}}

	backoff := sub.pollBackoff(2)
	if backoff != time.Duration(1<<63-1) {
		t.Fatalf("expected saturated max duration, got %s", backoff)
	}
}

func TestSubscription_PollBackoffExponentialSequence(t *testing.T) {
	sub := &Subscription[int64]{cfg: &Config{
		PollInterval:     10 * time.Millisecond,
		PollRetryBackoff: 5 * time.Millisecond,
	}}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 5 * time.Millisecond},
		{attempt: 2, want: 10 * time.Millisecond},
		{attempt: 3, want: 20 * time.Millisecond},
		{attempt: 4, want: 40 * time.Millisecond},
		{attempt: 6, want: 160 * time.Millisecond},
		{attempt: 7, want: 160 * time.Millisecond},
	}
	for _, tt := range tests {
		if got := sub.pollBackoff(tt.attempt); got != tt.want {
			t.Fatalf("attempt %d backoff = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

type opaqueCursorStreamStore struct {
	*store.MemoryEventStore[int64]
	events []eventing.Event[int64]
	after  string
}

func (s *opaqueCursorStreamStore) StreamEvents(_ context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	if opts != nil {
		s.after = opts.After
	}
	if s.after == "pos:2" {
		return &store.StreamResult[int64]{}, nil
	}
	return &store.StreamResult[int64]{
		Events:       s.events,
		EventCursors: []string{"pos:1", "pos:2"},
		NextCursor:   "pos:2",
	}, nil
}

func TestSubscription_Run_UsesStoreComputedCursor(t *testing.T) {
	e1 := testutil.NewEvent[int64](1, "Agg", "event-a", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "event-b", 2, nil)
	es := &opaqueCursorStreamStore{
		MemoryEventStore: store.NewMemoryEventStore[int64](),
		events:           []eventing.Event[int64]{*e1, *e2},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := 0
	sub, err := New[int64](es, func(context.Context, eventing.Event[int64]) error {
		seen++
		if seen == 2 {
			cancel()
		}
		return nil
	}, &Config{PollInterval: time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(ctx)

	if got := sub.Cursor(); got != "pos:2" {
		t.Fatalf("expected store-computed cursor pos:2, got %q", got)
	}
}

type emptyPageCursorStreamStore struct {
	*store.MemoryEventStore[int64]
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (s *emptyPageCursorStreamStore) StreamEvents(_ context.Context, opts *store.StreamOptions) (*store.StreamResult[int64], error) {
	s.calls.Add(1)
	if opts != nil && opts.After == "pos:empty" {
		if s.cancel != nil {
			s.cancel()
		}
		return &store.StreamResult[int64]{}, nil
	}
	return &store.StreamResult[int64]{
		NextCursor: "pos:empty",
	}, nil
}

func TestSubscription_Run_AdvancesCursorOnEmptyPageWithNextCursor(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	es := &emptyPageCursorStreamStore{
		MemoryEventStore: store.NewMemoryEventStore[int64](),
		cancel:           cancel,
	}
	cursorStore := &memoryCursorStore{cursors: map[string]string{}}
	sub, err := New[int64](es, func(context.Context, eventing.Event[int64]) error {
		t.Fatal("handler should not be called for an empty page")
		return nil
	}, &Config{
		Name:         "orders",
		CursorStore:  cursorStore,
		PollInterval: time.Millisecond,
		BatchSize:    10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(runCtx)

	if got := sub.Cursor(); got != "pos:empty" {
		t.Fatalf("expected cursor to advance to empty page cursor, got %q", got)
	}
	if got := cursorStore.cursors["orders"]; got != "pos:empty" {
		t.Fatalf("expected persisted cursor pos:empty, got %q", got)
	}
	if got := es.calls.Load(); got < 2 {
		t.Fatalf("expected subscription to poll again with advanced cursor, got %d calls", got)
	}
}

func TestSubscription_Run_ConvertsHandlerPanicToErrorWithoutAdvancingCursor(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	sub, err := New[int64](es, func(context.Context, eventing.Event[int64]) error {
		panic("boom")
	}, &Config{PollInterval: time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	err = sub.Run(ctx)
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected handler panic to become Internal error, got %v", err)
	}
	if got := sub.Cursor(); got != "" {
		t.Fatalf("expected cursor not to advance after handler panic, got %s", got)
	}
}

func TestSubscription_Run_PanicDoesNotRetryOrDeadLetterAndCapturesStack(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	var (
		attempts        int
		deadLetterCalls int
	)
	sub, err := New[int64](es, func(context.Context, eventing.Event[int64]) error {
		attempts++
		panic("boom")
	}, &Config{
		PollInterval:              time.Millisecond,
		BatchSize:                 10,
		MaxHandlerRetries:         3,
		SkipFailedAfterMaxRetries: true,
		DeadLetterFunc: func(err error, evt eventing.IEvent) error {
			deadLetterCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	err = sub.Run(ctx)
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected handler panic to become Internal error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected panic not to retry, got %d attempts", attempts)
	}
	if deadLetterCalls != 0 {
		t.Fatalf("expected panic not to be dead-lettered, got %d calls", deadLetterCalls)
	}
	if got := sub.Cursor(); got != "" {
		t.Fatalf("expected cursor not to advance after handler panic, got %s", got)
	}

	var appErr *errors.AppError
	if !errors.As(err, &appErr) || appErr == nil {
		t.Fatalf("expected AppError, got %T", err)
	}
	stack, ok := appErr.Details()["panic_stack"].(string)
	if !ok || stack == "" {
		t.Fatalf("expected panic_stack in error details, got %v", appErr.Details())
	}
}

// TestSubscription_Run_ConsumesEvents 验证 Subscription Run ConsumesEvents。
func TestSubscription_Run_ConsumesEvents(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu     sync.Mutex
		types  []string
		seenID = make(map[string]struct{})
	)
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := seenID[evt.GetID()]; ok {
			t.Fatalf("duplicate event delivered: %s", evt.GetID())
		}
		seenID[evt.GetID()] = struct{}{}
		types = append(types, evt.GetType())
		if len(types) == 2 {
			cancel()
		}
		return nil
	}, &Config{
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
		FromTime:     time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	// 先写入两条事件
	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, map[string]any{"x": 1})
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, map[string]any{"x": 2})
	if err := es.AppendEvents(context.Background(), "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	_ = sub.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(types) != 2 {
		t.Fatalf("expected 2 events, got %d", len(types))
	}
	if types[0] != "A" || types[1] != "B" {
		t.Fatalf("unexpected event types: %v", types)
	}
	if sub.Cursor() == "" {
		t.Fatalf("expected cursor to be set")
	}
}
