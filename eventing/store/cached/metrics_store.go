package cached

import (
	"context"
	"time"

	"gochen/eventing"
	estore "gochen/eventing/store"
)

// IMetricsRecorder 抽象化的指标记录器，便于对接 Prometheus 或自定义埋点
type IMetricsRecorder interface {
	RecordAppend(count int, d time.Duration, err bool)
	RecordLoad(count int, d time.Duration, err bool)
	RecordStream(count int, d time.Duration, err bool)
}

// MetricsEventStore 为任意 EventStore 增加简单的指标记录
type MetricsEventStore struct {
	inner estore.IEventStore[int64]
	mr    IMetricsRecorder
}

// NewMetricsEventStore 为 IEventStore[int64] 增加简单的指标记录能力。
func NewMetricsEventStore(inner estore.IEventStore[int64], mr IMetricsRecorder) *MetricsEventStore {
	if inner == nil {
		panic("NewMetricsEventStore: inner IEventStore cannot be nil")
	}
	return &MetricsEventStore{inner: inner, mr: mr}
}

func (m *MetricsEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent[int64], expectedVersion uint64) error {
	start := time.Now()
	err := m.inner.AppendEvents(ctx, aggregateID, events, expectedVersion)
	if m.mr != nil {
		m.mr.RecordAppend(len(events), time.Since(start), err != nil)
	}
	return err
}

func (m *MetricsEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	start := time.Now()
	evs, err := m.inner.LoadEvents(ctx, aggregateID, afterVersion)
	if m.mr != nil {
		m.mr.RecordLoad(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

func (m *MetricsEventStore) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event[int64], error) {
	start := time.Now()
	evs, err := m.inner.StreamEvents(ctx, from)
	if m.mr != nil {
		m.mr.RecordStream(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

// GetEventStreamWithCursor 若底层支持扩展接口则委托，否则回退到 StreamEvents 并应用过滤
func (m *MetricsEventStore) GetEventStreamWithCursor(ctx context.Context, opts *estore.StreamOptions) (*estore.StreamResult[int64], error) {
	start := time.Now()
	var (
		res *estore.StreamResult[int64]
		err error
	)
	if extended, ok := m.inner.(estore.IEventStreamStore[int64]); ok {
		res, err = extended.GetEventStreamWithCursor(ctx, opts)
	} else {
		var evs []eventing.Event[int64]
		evs, err = m.inner.StreamEvents(ctx, opts.FromTime)
		if err == nil {
			res = estore.FilterEventsWithOptions[int64](evs, opts)
		}
	}

	if m.mr != nil {
		count := 0
		if res != nil {
			count = len(res.Events)
		}
		m.mr.RecordStream(count, time.Since(start), err != nil)
	}
	return res, err
}

// StreamAggregate 按聚合顺序流式读取事件（委托到底层存储）
func (m *MetricsEventStore) StreamAggregate(ctx context.Context, opts *estore.AggregateStreamOptions[int64]) (*estore.AggregateStreamResult[int64], error) {
	if streamStore, ok := m.inner.(estore.IEventStreamStore[int64]); ok {
		return streamStore.StreamAggregate(ctx, opts)
	}
	// 底层不支持时返回错误
	return nil, nil
}

// 接口断言
var _ estore.IEventStreamStore[int64] = (*MetricsEventStore)(nil)
