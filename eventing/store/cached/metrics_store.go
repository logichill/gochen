package cached

import (
	"context"
	"time"

	"gochen/eventing"
	estore "gochen/eventing/store"
)

// MetricsRecorder 抽象化的指标记录器，便于对接 Prometheus 或自定义埋点
type MetricsRecorder interface {
	RecordAppend(count int, d time.Duration, err bool)
	RecordLoad(count int, d time.Duration, err bool)
	RecordStream(count int, d time.Duration, err bool)
}

// MetricsEventStore 为任意 EventStore 增加简单的指标记录
type MetricsEventStore struct {
	inner estore.IEventStore
	mr    MetricsRecorder
}

func NewMetricsEventStore(inner estore.IEventStore, mr MetricsRecorder) *MetricsEventStore {
	return &MetricsEventStore{inner: inner, mr: mr}
}

func (m *MetricsEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error {
	start := time.Now()
	err := m.inner.AppendEvents(ctx, aggregateID, events, expectedVersion)
	if m.mr != nil {
		m.mr.RecordAppend(len(events), time.Since(start), err != nil)
	}
	return err
}

func (m *MetricsEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error) {
	start := time.Now()
	evs, err := m.inner.LoadEvents(ctx, aggregateID, afterVersion)
	if m.mr != nil {
		m.mr.RecordLoad(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

func (m *MetricsEventStore) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event, error) {
	start := time.Now()
	evs, err := m.inner.StreamEvents(ctx, from)
	if m.mr != nil {
		m.mr.RecordStream(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}
