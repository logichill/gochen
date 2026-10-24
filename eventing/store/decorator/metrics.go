package decorator

import (
	"context"
	"reflect"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
)

// IMetricsRecorder 抽象指标Recorder能力接口。
type IMetricsRecorder interface {
	RecordAppend(count int, d time.Duration, err bool)
	RecordLoad(count int, d time.Duration, err bool)
	RecordStream(count int, d time.Duration, err bool)
}

// IAggregateMetricsRecorder 可选扩展：单聚合流式读取与存在性/版本探查的高频
// 指标打点。实现方可按需实现该接口，装饰器经类型断言探测；未实现时这些
// 调用保持穿透但不产生指标（保持向后兼容，不破坏既有 IMetricsRecorder 实现）。
type IAggregateMetricsRecorder interface {
	RecordAggregateStream(count int, d time.Duration, err bool)
	RecordAggregateProbe(d time.Duration, err bool)
}

// MetricsEventStore 定义指标事件存储。
type MetricsEventStore[ID comparable] struct {
	inner store.IEventStreamStore[ID]
	mr    IMetricsRecorder
}

// NewMetricsEventStore 创建指标事件存储。
func NewMetricsEventStore[ID comparable](inner store.IEventStreamStore[ID], mr IMetricsRecorder) (*MetricsEventStore[ID], error) {
	if isNil(inner) {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	if isNil(mr) {
		mr = nil
	}
	return &MetricsEventStore[ID]{inner: inner, mr: mr}, nil
}

// AppendEvents 向事件存储追加事件。
func (m *MetricsEventStore[ID]) AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if m == nil || isNil(m.inner) {
		return errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	err := m.inner.AppendEvents(ctx, aggregateType, aggregateID, events, expectedVersion)
	if !isNil(m.mr) {
		m.mr.RecordAppend(len(events), time.Since(start), err != nil)
	}
	return err
}

// LoadEvents 加载聚合事件。
func (m *MetricsEventStore[ID]) LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if m == nil || isNil(m.inner) {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	evs, err := m.inner.LoadEvents(ctx, aggregateType, aggregateID, afterVersion)
	if !isNil(m.mr) {
		m.mr.RecordLoad(len(evs), time.Since(start), err != nil)
	}
	return evs, err
}

func (m *MetricsEventStore[ID]) StreamEvents(ctx context.Context, opts *store.StreamOptions) (*store.StreamResult[ID], error) {
	if m == nil || isNil(m.inner) {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	var (
		res *store.StreamResult[ID]
		err error
	)
	res, err = m.inner.StreamEvents(ctx, opts)

	if !isNil(m.mr) {
		count := 0
		if res != nil {
			count = len(res.Events)
		}
		m.mr.RecordStream(count, time.Since(start), err != nil)
	}
	return res, err
}

// StreamAggregate 按聚合顺序流式读取事件（委托到底层存储）。
func (m *MetricsEventStore[ID]) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[ID]) (*store.AggregateStreamResult[ID], error) {
	if m == nil || isNil(m.inner) {
		return nil, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	res, err := m.inner.StreamAggregate(ctx, opts)
	if rec, ok := any(m.mr).(IAggregateMetricsRecorder); ok && !isNil(m.mr) {
		count := 0
		if res != nil {
			count = len(res.Events)
		}
		rec.RecordAggregateStream(count, time.Since(start), err != nil)
	}
	return res, err
}

// HasAggregate 检查聚合是否存在。
func (m *MetricsEventStore[ID]) HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error) {
	if m == nil || isNil(m.inner) {
		return false, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	exists, err := m.inner.HasAggregate(ctx, aggregateType, aggregateID)
	if rec, ok := any(m.mr).(IAggregateMetricsRecorder); ok && !isNil(m.mr) {
		rec.RecordAggregateProbe(time.Since(start), err != nil)
	}
	return exists, err
}

func (m *MetricsEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error) {
	if m == nil || isNil(m.inner) {
		return 0, errors.NewCode(errors.InvalidInput, "inner event store cannot be nil")
	}
	start := time.Now()
	version, err := m.inner.GetAggregateVersion(ctx, aggregateType, aggregateID)
	if rec, ok := any(m.mr).(IAggregateMetricsRecorder); ok && !isNil(m.mr) {
		rec.RecordAggregateProbe(time.Since(start), err != nil)
	}
	return version, err
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// 接口断言。
var _ store.IEventStreamStore[int64] = (*MetricsEventStore[int64])(nil)
