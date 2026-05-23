package cached

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/eventing"
	estore "gochen/eventing/store"
)

// IMetricsRecorder 抽象指标Recorder能力接口。
type IMetricsRecorder interface {
	RecordAppend(count int, d time.Duration, err bool)
	RecordLoad(count int, d time.Duration, err bool)
	RecordStream(count int, d time.Duration, err bool)
}

// MetricsEventStore 定义指标事件存储。
type MetricsEventStore[ID comparable] struct {
	inner estore.IEventStreamStore[ID]
	mr    IMetricsRecorder
}

// NewMetricsEventStore 创建指标事件存储。
func NewMetricsEventStore[ID comparable](inner estore.IEventStreamStore[ID], mr IMetricsRecorder) (*MetricsEventStore[ID], error) {
	if inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	return &MetricsEventStore[ID]{inner: inner, mr: mr}, nil
}

// AppendEvents 向事件存储追加事件。
func (m *MetricsEventStore[ID]) AppendEvents(ctx context.Context, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	start := time.Now()
	err := m.inner.AppendEvents(ctx, aggregateID, events, expectedVersion)
	if m.mr != nil {
		m.mr.RecordAppend(len(events), time.Since(start), err != nil)
	}
	return err
}

// LoadEvents 加载聚合事件。
func (m *MetricsEventStore[ID]) LoadEvents(ctx context.Context, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	start := time.Now()
	evs, err := m.inner.LoadEvents(ctx, aggregateID, afterVersion)
	if m.mr != nil {
		m.mr.RecordLoad(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

// LoadEventsByType 加载指定聚合类型的事件。
func (m *MetricsEventStore[ID]) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	start := time.Now()
	evs, err := m.inner.LoadEventsByType(ctx, aggregateType, aggregateID, afterVersion)
	if m.mr != nil {
		m.mr.RecordLoad(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

func (m *MetricsEventStore[ID]) StreamEvents(ctx context.Context, opts *estore.StreamOptions) (*estore.StreamResult[ID], error) {
	start := time.Now()
	var (
		res *estore.StreamResult[ID]
		err error
	)
	res, err = m.inner.StreamEvents(ctx, opts)

	if m.mr != nil {
		count := 0
		if res != nil {
			count = len(res.Events)
		}
		m.mr.RecordStream(count, time.Since(start), err != nil)
	}
	return res, err
}

// StreamAggregate 按聚合顺序流式读取事件（委托到底层存储）。
func (m *MetricsEventStore[ID]) StreamAggregate(ctx context.Context, opts *estore.AggregateStreamOptions[ID]) (*estore.AggregateStreamResult[ID], error) {
	return m.inner.StreamAggregate(ctx, opts)
}

// HasAggregate 检查聚合是否存在。
func (m *MetricsEventStore[ID]) HasAggregate(ctx context.Context, aggregateID ID) (bool, error) {
	return m.inner.HasAggregate(ctx, aggregateID)
}

func (m *MetricsEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error) {
	return m.inner.GetAggregateVersion(ctx, aggregateID)
}

// 接口断言。
var _ estore.IEventStreamStore[int64] = (*MetricsEventStore[int64])(nil)
