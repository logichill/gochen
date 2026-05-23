package cached

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/store"
)

type fakeRecorder struct {
	streamCount int
	streamErr   bool
}

// RecordAppend 执行对应操作。
//
// 参数：
// - d：参数值（具体语义见函数上下文）（类型：time.Duration）
// - err：待检查/包装的错误（类型：bool）
func (f *fakeRecorder) RecordAppend(count int, d time.Duration, err bool) {}

// RecordLoad 执行对应操作。
//
// 参数：
// - d：参数值（具体语义见函数上下文）（类型：time.Duration）
// - err：待检查/包装的错误（类型：bool）
func (f *fakeRecorder) RecordLoad(count int, d time.Duration, err bool) {}

// RecordStream 执行对应操作。
//
// 参数：
// - d：参数值（具体语义见函数上下文）（类型：time.Duration）
// - err：待检查/包装的错误（类型：bool）
func (f *fakeRecorder) RecordStream(count int, d time.Duration, err bool) {
	f.streamCount = count
	f.streamErr = err
}

// TestMetricsEventStore_StreamEvents 验证 MetricsEventStore StreamEvents。
func TestMetricsEventStore_StreamEvents(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemoryEventStore()
	recorder := &fakeRecorder{}
	ms, err := NewMetricsEventStore(mem, recorder)
	require.NoError(t, err)

	e1 := eventing.NewEvent[int64](1, "Agg", "TypeA", 1, nil)
	e2 := eventing.NewEvent[int64](1, "Agg", "TypeB", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	require.NoError(t, mem.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	res, err := ms.StreamEvents(ctx, &store.StreamOptions{
		After: e1.ID,
		Types: []string{"TypeB"},
	})
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	require.Equal(t, e2.ID, res.Events[0].ID)
	require.False(t, recorder.streamErr)
	require.Equal(t, 1, recorder.streamCount)
}

func TestNewMetricsEventStore_ReturnsErrorOnNilInner(t *testing.T) {
	_, err := NewMetricsEventStore[int64](nil, &fakeRecorder{})
	require.Error(t, err)
}
