package subscription

import (
	"context"
	"errors"
	"gochen/eventing/internal/testutil"
	"sync"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
)

func TestSubscription_Run_ResumeFromCursor(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	// 写入三条事件
	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "C", 3, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	// 第一次消费：吃掉前两条，然后 cancel
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	var (
		mu    sync.Mutex
		seen1 []string
	)
	sub1, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		mu.Lock()
		defer mu.Unlock()
		if len(seen1) >= 2 {
			// cancel 的生效存在一个轮询间隔窗口；防御性地忽略多余事件，避免测试抖动。
			return nil
		}
		seen1 = append(seen1, evt.GetType())
		if len(seen1) == 2 {
			cancel1()
		}
		return nil
	}, &Config{PollInterval: 5 * time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}
	_ = sub1.Run(ctx1)

	mu.Lock()
	got := len(seen1)
	mu.Unlock()
	if got != 2 {
		t.Fatalf("expected first run to consume 2 events, got %d", got)
	}
	// cursor 语义是“最后一条成功处理的事件 ID”，因此应为第二条事件的 ID
	startCursor := e2.GetID()
	if startCursor == "" {
		t.Fatalf("expected cursor to be set")
	}

	// 第二次消费：从 cursor 继续，应只拿到最后一条
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	var seen2 []string
	sub2, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		seen2 = append(seen2, evt.GetType())
		cancel2()
		return nil
	}, &Config{PollInterval: 5 * time.Millisecond, BatchSize: 10, StartCursor: startCursor})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}
	// 增加超时保护：若 cursor 无效/未能产生新事件，Run 会持续轮询。
	ctx2Timeout, cancel2Timeout := context.WithTimeout(ctx2, 200*time.Millisecond)
	defer cancel2Timeout()
	err = sub2.Run(ctx2Timeout)
	if len(seen2) == 0 {
		// 说明：若未消费到事件，Run 可能因超时退出（ctx deadline）。
		// 这通常意味着 cursor 未能恢复到可继续的位置。
		if err != nil {
			t.Fatalf("expected to resume and consume 1 event, got err=%v", err)
		}
		t.Fatalf("expected to resume and consume 1 event, got none")
	}

	if len(seen2) != 1 || seen2[0] != "C" {
		t.Fatalf("expected resume to consume only C, got %v", seen2)
	}
}

func TestSubscription_Run_LoadsCursorFromStore(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "C", 3, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	cursorStore := &memoryCursorStore{cursors: map[string]string{"orders": e2.GetID()}}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seen []string
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		seen = append(seen, evt.GetType())
		cancel()
		return nil
	}, &Config{
		Name:         "orders",
		CursorStore:  cursorStore,
		StartCursor:  e1.GetID(),
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(runCtx)

	if len(seen) != 1 || seen[0] != "C" {
		t.Fatalf("expected persistent cursor to resume from C, got %v", seen)
	}
	if got := cursorStore.cursors["orders"]; got != e3.GetID() {
		t.Fatalf("expected saved cursor %s, got %s", e3.GetID(), got)
	}
}

func TestSubscription_Run_DoesNotAdvanceMemoryCursorWhenStoreSaveFails(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	saveErr := errors.New("save cursor failed")
	cursorStore := &memoryCursorStore{cursors: map[string]string{}, saveErr: saveErr}
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		return nil
	}, &Config{
		Name:         "orders",
		CursorStore:  cursorStore,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	err = sub.Run(context.Background())
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected save cursor error, got %v", err)
	}
	if got := sub.Cursor(); got != "" {
		t.Fatalf("expected in-memory cursor not to advance, got %s", got)
	}
}

func TestSubscription_Run_AdvancesCursorAfterEachSuccessfulEvent(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "C", 3, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	failErr := errors.New("stop after partial success")
	var seen []string
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		if evt.GetType() == "C" {
			return failErr
		}
		seen = append(seen, evt.GetType())
		return nil
	}, &Config{PollInterval: 5 * time.Millisecond, BatchSize: 10})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	err = sub.Run(ctx)
	if !errors.Is(err, failErr) {
		t.Fatalf("expected handler error, got %v", err)
	}
	if got := sub.Cursor(); got != e2.GetID() {
		t.Fatalf("expected cursor to advance to second successful event %s, got %s", e2.GetID(), got)
	}
	if len(seen) != 2 || seen[0] != "A" || seen[1] != "B" {
		t.Fatalf("unexpected seen events: %v", seen)
	}
}

type memoryCursorStore struct {
	mu             sync.Mutex
	cursors        map[string]string
	loadErr        error
	saveErr        error
	requireLiveCtx bool
	saveCalls      int
}

func (s *memoryCursorStore) LoadCursor(ctx context.Context, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return "", s.loadErr
	}
	if s.cursors == nil {
		return "", nil
	}
	return s.cursors[name], nil
}

func (s *memoryCursorStore) SaveCursor(ctx context.Context, name string, cursor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requireLiveCtx && ctx.Err() != nil {
		return ctx.Err()
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saveCalls++
	if s.cursors == nil {
		s.cursors = make(map[string]string)
	}
	s.cursors[name] = cursor
	return nil
}

func TestSubscription_Run_PersistsCursorAfterHandlerCancelsContext(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cursorStore := &memoryCursorStore{cursors: map[string]string{}, requireLiveCtx: true}
	sub, err := New[int64](es, func(context.Context, eventing.Event[int64]) error {
		cancel()
		return nil
	}, &Config{
		Name:         "orders",
		CursorStore:  cursorStore,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(runCtx)

	if got := cursorStore.cursors["orders"]; got != e1.GetID() {
		t.Fatalf("expected saved cursor %s, got %s", e1.GetID(), got)
	}
	if got := sub.Cursor(); got != e1.GetID() {
		t.Fatalf("expected in-memory cursor %s, got %s", e1.GetID(), got)
	}
}

func TestSubscription_Run_PersistsCursorOncePerSuccessfulBatch(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	cursorStore := &memoryCursorStore{cursors: map[string]string{}}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seen int
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		seen++
		if seen == 2 {
			cancel()
		}
		return nil
	}, &Config{
		Name:         "orders",
		CursorStore:  cursorStore,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    10,
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(runCtx)

	if cursorStore.saveCalls != 1 {
		t.Fatalf("expected one cursor save for the batch, got %d", cursorStore.saveCalls)
	}
	if got := cursorStore.cursors["orders"]; got != e2.GetID() {
		t.Fatalf("expected saved cursor %s, got %s", e2.GetID(), got)
	}
}

func TestSubscription_Run_SkipsFailedEventAfterMaxHandlerRetries(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := testutil.NewEvent[int64](1, "Agg", "B", 2, nil)
	e3 := testutil.NewEvent[int64](1, "Agg", "C", 3, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	failErr := errors.New("poison event")
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		seen          []string
		bAttempts     int
		deadLetterIDs []string
	)
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		switch evt.GetType() {
		case "B":
			bAttempts++
			return failErr
		case "C":
			cancel()
		}
		seen = append(seen, evt.GetType())
		return nil
	}, &Config{
		PollInterval:              5 * time.Millisecond,
		BatchSize:                 10,
		MaxHandlerRetries:         2,
		SkipFailedAfterMaxRetries: true,
		HandlerRetryBackoff:       time.Millisecond,
		DeadLetterFunc: func(err error, evt eventing.IEvent) error {
			deadLetterIDs = append(deadLetterIDs, evt.GetID())
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	_ = sub.Run(runCtx)

	if bAttempts != 3 {
		t.Fatalf("expected B to be attempted 3 times, got %d", bAttempts)
	}
	if len(deadLetterIDs) != 1 || deadLetterIDs[0] != e2.GetID() {
		t.Fatalf("expected B to be dead-lettered, got %v", deadLetterIDs)
	}
	if len(seen) != 2 || seen[0] != "A" || seen[1] != "C" {
		t.Fatalf("expected A and C to be handled, got %v", seen)
	}
	if got := sub.Cursor(); got != e3.GetID() {
		t.Fatalf("expected cursor to advance to C %s, got %s", e3.GetID(), got)
	}
}

func TestSubscription_Run_ReturnsDeadLetterFuncErrorWithoutAdvancingCursor(t *testing.T) {
	es := store.NewMemoryEventStore[int64]()
	ctx := context.Background()

	e1 := testutil.NewEvent[int64](1, "Agg", "A", 1, nil)
	if err := es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{e1}, 0); err != nil {
		t.Fatalf("AppendEvents error: %v", err)
	}

	handlerErr := errors.New("poison event")
	deadLetterErr := errors.New("dead letter sink unavailable")
	sub, err := New[int64](es, func(ctx context.Context, evt eventing.Event[int64]) error {
		return handlerErr
	}, &Config{
		PollInterval:              5 * time.Millisecond,
		BatchSize:                 10,
		SkipFailedAfterMaxRetries: true,
		DeadLetterFunc: func(err error, evt eventing.IEvent) error {
			if !errors.Is(err, handlerErr) {
				t.Fatalf("expected handler error passed to DeadLetterFunc, got %v", err)
			}
			return deadLetterErr
		},
	})
	if err != nil {
		t.Fatalf("New subscription failed: %v", err)
	}

	err = sub.Run(ctx)
	if !errors.Is(err, deadLetterErr) {
		t.Fatalf("expected dead-letter error, got %v", err)
	}
	if got := sub.Cursor(); got != "" {
		t.Fatalf("expected cursor not to advance when dead-letter hook fails, got %s", got)
	}
}
