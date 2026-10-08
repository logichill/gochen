package eventstore_test

import (
	"context"
	"strconv"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/testkit/require"
)

// TestGetEventHistoryPage_PaginatesByVersion 验证 esstore.EventHistoryPage PaginatesByVersion。
func TestGetEventHistoryPage_PaginatesByVersion(t *testing.T) {
	ctx := context.Background()

	es := store.NewMemoryEventStore[int64]()
	for i := 1; i <= 3; i++ {
		evt := newTestEvent[int64](1, "Agg", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, "Agg", 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}

	page1, err := esstore.LoadEventHistoryPage(ctx, es, "Agg", 1, 1, 2, nil)
	require.NoError(t, err)
	require.Equal(t, 3, page1.Total)
	require.Len(t, page1.Entries, 2)

	page2, err := esstore.LoadEventHistoryPage(ctx, es, "Agg", 1, 2, 2, nil)
	require.NoError(t, err)
	require.Equal(t, 3, page2.Total)
	require.Len(t, page2.Entries, 1)
}

// TestLoadEventHistoryPage_OverflowingPageReturnsEmptyPage 回归 R1c1-F02：
// page 为调用方可控时，(page-1)*pageSize 会整型溢出回绕（如 64 位下
// ((1<<62)+1-1)*20 ≡ 0 (mod 2^64)），修复前超大页码会静默把第 1 页数据
// 当作该页返回；修复后应按超范围页语义返回空页。
func TestLoadEventHistoryPage_OverflowingPageReturnsEmptyPage(t *testing.T) {
	ctx := context.Background()

	es := store.NewMemoryEventStore[int64]()
	for i := 1; i <= 3; i++ {
		evt := newTestEvent[int64](7, "Order", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, "Order", 7, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}

	// page-1 取 1<<(IntSize-2)：乘 pageSize=20 后按字长回绕为 0，
	// 恰好落回第 1 页偏移，是缺陷最隐蔽的触发形态（32/64 位平台通用）。
	hugePage := int(1)<<(strconv.IntSize-2) + 1
	page, err := esstore.LoadEventHistoryPage(ctx, es, "Order", 7, hugePage, 20, nil)
	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, hugePage, page.Page)
	require.Len(t, page.Entries, 0)

	// 对照：未溢出的超大页码（page=1000000）本就返回空页，修复不应改变其行为。
	control, err := esstore.LoadEventHistoryPage(ctx, es, "Order", 7, 1000000, 20, nil)
	require.NoError(t, err)
	require.Equal(t, 3, control.Total)
	require.Len(t, control.Entries, 0)
}

func TestGetEventHistoryPage_TotalIsScopedByAggregateType(t *testing.T) {
	ctx := context.Background()

	es := store.NewMemoryEventStore[int64]()
	for i := 1; i <= 2; i++ {
		evt := newTestEvent[int64](1, "AggA", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, "AggA", 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}
	for i := 1; i <= 3; i++ {
		evt := newTestEvent[int64](2, "AggB", "E", uint64(i), map[string]any{"i": i})
		evt.SchemaVersion = 1
		require.NoError(t, es.AppendEvents(ctx, "AggB", 2, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)))
	}

	page, err := esstore.LoadEventHistoryPage(ctx, es, "AggA", 1, 1, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Entries, 2)
	for _, entry := range page.Entries {
		require.Equal(t, "AggA", entry.AggregateType)
	}
}
