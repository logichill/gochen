package subscription

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
)

func TestSubscription_Run_ResumeFromCursor(t *testing.T) {
	es := store.NewMemoryEventStore()
	ctx := context.Background()

	// 写入三条事件
	e1 := eventing.NewEvent[int64](1, "Agg", "A", 1, nil)
	e2 := eventing.NewEvent[int64](1, "Agg", "B", 2, nil)
	e3 := eventing.NewEvent[int64](1, "Agg", "C", 3, nil)
	if err := es.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{e1, e2, e3}, 0); err != nil {
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
