package decorator

import (
	"context"
	stderrors "errors"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/store"
	"gochen/testkit/require"
	"testing"
	"time"
)

type closableEventStore struct {
	store.IEventStreamStore[int64]
	closeCalls int
	closeErr   error
}

func (c *closableEventStore) Close() error {
	c.closeCalls++
	return c.closeErr
}

type nonAtomicEventStore struct {
	store.IEventStreamStore[int64]
}

type fullRecorder struct {
	appendCount  int
	appendErr    bool
	loadCount    int
	loadErr      bool
	streamCount  int
	streamErr    bool
	aggStreamCnt int
	aggStreamErr bool
	probeCalls   int
	probeErr     bool
}

func (r *fullRecorder) RecordAppend(count int, _ time.Duration, err bool) {
	r.appendCount = count
	r.appendErr = err
}

func (r *fullRecorder) RecordLoad(count int, _ time.Duration, err bool) {
	r.loadCount = count
	r.loadErr = err
}

func (r *fullRecorder) RecordStream(count int, _ time.Duration, err bool) {
	r.streamCount = count
	r.streamErr = err
}

func (r *fullRecorder) RecordAggregateStream(count int, _ time.Duration, err bool) {
	r.aggStreamCnt = count
	r.aggStreamErr = err
}

func (r *fullRecorder) RecordAggregateProbe(_ time.Duration, err bool) {
	r.probeCalls++
	r.probeErr = err
}

func TestDecorators_ClosePropagatesThroughChainAndHandlesNil(t *testing.T) {
	var nilCtx *ContextAwareEventStore[int64]
	var nilTenant *TenantAwareEventStore[int64]
	var nilTracing *TracingEventStore[int64]
	var nilMetrics *MetricsEventStore[int64]

	require.NoError(t, nilCtx.Close())
	require.NoError(t, nilTenant.Close())
	require.NoError(t, nilTracing.Close())
	require.NoError(t, nilMetrics.Close())
	require.NoError(t, closeInnerStore(nil))

	// 底层未实现 Close 时静默返回 nil
	nonClosable := &nonAtomicEventStore{IEventStreamStore: store.NewMemoryEventStore[int64]()}
	require.NoError(t, NewContextAwareEventStore[int64](nonClosable).Close())

	// 组合链 Metrics -> Tracing -> Tenant -> Context -> closable 透传关闭与错误
	inner := &closableEventStore{IEventStreamStore: store.NewMemoryEventStore[int64]()}
	ctxStore := NewContextAwareEventStore[int64](inner)
	tenantStore := NewTenantAwareEventStore[int64](ctxStore)
	tracingStore := NewTracingEventStore[int64](tenantStore)
	metricsStore, err := NewMetricsEventStore[int64](tracingStore, nil)
	require.NoError(t, err)

	require.NoError(t, metricsStore.Close())
	require.Equal(t, 1, inner.closeCalls)

	wantErr := stderrors.New("close boom")
	inner.closeErr = wantErr
	require.ErrorIs(t, metricsStore.Close(), wantErr)
	require.Equal(t, 2, inner.closeCalls)
}

func TestDecorators_AppendEventsBatch(t *testing.T) {
	t.Run("NilAndUnsupported", func(t *testing.T) {
		ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
		require.NoError(t, err)

		var nilMetrics *MetricsEventStore[int64]
		var nilCtxStore *ContextAwareEventStore[int64]
		var nilTenantStore *TenantAwareEventStore[int64]
		var nilTracingStore *TracingEventStore[int64]

		require.True(t, errors.Is(nilMetrics.AppendEventsBatch(ctx, nil), errors.InvalidInput))
		require.True(t, errors.Is(nilCtxStore.AppendEventsBatch(ctx, nil), errors.InvalidInput))
		require.True(t, errors.Is(nilTenantStore.AppendEventsBatch(ctx, nil), errors.InvalidInput))
		require.True(t, errors.Is(nilTracingStore.AppendEventsBatch(ctx, nil), errors.InvalidInput))

		nonAtomic := &nonAtomicEventStore{IEventStreamStore: store.NewMemoryEventStore[int64]()}
		ms, err := NewMetricsEventStore[int64](nonAtomic, nil)
		require.NoError(t, err)
		require.True(t, errors.Is(ms.AppendEventsBatch(ctx, nil), errors.Unsupported))
		require.True(t, errors.Is(NewContextAwareEventStore[int64](nonAtomic).AppendEventsBatch(ctx, nil), errors.Unsupported))
		require.True(t, errors.Is(NewTenantAwareEventStore[int64](nonAtomic).AppendEventsBatch(ctx, nil), errors.Unsupported))
		require.True(t, errors.Is(NewTracingEventStore[int64](nonAtomic).AppendEventsBatch(ctx, nil), errors.Unsupported))
	})

	t.Run("ContextTracingMetricsBatchSuccessAndMetrics", func(t *testing.T) {
		base := store.NewMemoryEventStore[int64]()
		rec := &fullRecorder{}
		ctxStore := NewContextAwareEventStore[int64](base)
		tracingStore := NewTracingEventStore[int64](ctxStore)
		ms, err := NewMetricsEventStore[int64](tracingStore, rec)
		require.NoError(t, err)

		ctx := context.Background()
		ctx, err = contextx.WithTenantID(ctx, "tenant-a")
		require.NoError(t, err)
		ctx, err = contextx.WithOperator(ctx, "alice")
		require.NoError(t, err)
		ctx, err = contextx.WithTraceID(ctx, "trace-123")
		require.NoError(t, err)

		e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
		e2 := testutil.NewEvent[int64](2, "Order", "Created", 1, nil)
		batch := []store.EventAppend[int64]{
			{AggregateType: "Order", AggregateID: 1, Events: []eventing.IStorableEvent[int64]{e1}, ExpectedVersion: 0},
			{AggregateType: "Order", AggregateID: 2, Events: []eventing.IStorableEvent[int64]{e2}, ExpectedVersion: 0},
		}

		require.NoError(t, ms.AppendEventsBatch(ctx, batch))
		require.Equal(t, 2, rec.appendCount)
		require.False(t, rec.appendErr)

		loaded, err := ms.LoadEvents(ctx, "Order", 1, 0)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.Equal(t, 1, rec.loadCount)
		md := loaded[0].GetMetadata()
		tenantVal, _ := md.Get(contextx.MetadataTenantKey)
		opVal, _ := md.Get(contextx.MetadataOperatorKey)
		traceVal, ok := md.Get(contextx.MetadataTraceKey)
		require.Equal(t, "tenant-a", tenantVal)
		require.Equal(t, "alice", opVal)
		require.True(t, ok)
		require.Equal(t, "trace-123", traceVal)

		// 探查与聚合流指标打点
		exists, err := ms.HasAggregate(ctx, "Order", 1)
		require.NoError(t, err)
		require.True(t, exists)
		ver, err := ms.GetAggregateVersion(ctx, "Order", 1)
		require.NoError(t, err)
		require.Equal(t, uint64(1), ver)
		require.Equal(t, 2, rec.probeCalls)

		aggRes, err := ms.StreamAggregate(ctx, &store.AggregateStreamOptions[int64]{
			AggregateType: "Order",
			AggregateID:   1,
		})
		require.NoError(t, err)
		require.Len(t, aggRes.Events, 1)
		require.Equal(t, 1, rec.aggStreamCnt)
	})

	t.Run("TenantAwareBatchValidationAndSuccess", func(t *testing.T) {
		base := store.NewMemoryEventStore[int64]()
		ts := NewTenantAwareEventStore[int64](base)

		// 缺失租户上下文必须拒绝
		require.True(t, errors.Is(ts.AppendEventsBatch(context.Background(), nil), errors.InvalidInput))

		ctx, err := contextx.WithTenantID(context.Background(), "tenant-b")
		require.NoError(t, err)

		// nil 事件与 nil metadata 必须拒绝
		var typedNil *eventing.Event[int64]
		err = ts.AppendEventsBatch(ctx, []store.EventAppend[int64]{
			{AggregateType: "Order", AggregateID: 1, Events: []eventing.IStorableEvent[int64]{typedNil}},
		})
		require.True(t, errors.Is(err, errors.InvalidInput))

		err = ts.AppendEventsBatch(ctx, []store.EventAppend[int64]{
			{AggregateType: "Order", AggregateID: 1, Events: []eventing.IStorableEvent[int64]{&nilMetadataEvent{Event: testutil.NewEvent[int64](1, "Order", "Created", 1, nil)}}},
		})
		require.True(t, errors.Is(err, errors.InvalidInput))

		// 正常批量写入并按租户读取
		e1 := testutil.NewEvent[int64](10, "Order", "Created", 1, nil)
		require.NoError(t, ts.AppendEventsBatch(ctx, []store.EventAppend[int64]{
			{AggregateType: "Order", AggregateID: 10, Events: []eventing.IStorableEvent[int64]{e1}, ExpectedVersion: 0},
		}))

		has, err := ts.HasAggregate(ctx, "Order", 10)
		require.NoError(t, err)
		require.True(t, has)

		events, err := ts.LoadEvents(ctx, "Order", 10, 0)
		require.NoError(t, err)
		require.Len(t, events, 1)
		tenantVal, _ := events[0].GetMetadata().Get(contextx.MetadataTenantKey)
		require.Equal(t, "tenant-b", tenantVal)
	})

	t.Run("BatchTraceConflictRejected", func(t *testing.T) {
		base := store.NewMemoryEventStore[int64]()
		ctxStore := NewContextAwareEventStore[int64](base)
		tracingStore := NewTracingEventStore[int64](base)

		e1 := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
		e1.GetMetadata().Set(contextx.MetadataTraceKey, "trace-1")
		e2 := testutil.NewEvent[int64](2, "Order", "Created", 1, nil)
		e2.GetMetadata().Set(contextx.MetadataTraceKey, "trace-2")
		batch := []store.EventAppend[int64]{
			{AggregateType: "Order", AggregateID: 1, Events: []eventing.IStorableEvent[int64]{e1}},
			{AggregateType: "Order", AggregateID: 2, Events: []eventing.IStorableEvent[int64]{e2}},
		}

		require.True(t, errors.Is(ctxStore.AppendEventsBatch(context.Background(), batch), errors.InvalidInput))
		require.True(t, errors.Is(tracingStore.AppendEventsBatch(context.Background(), batch), errors.InvalidInput))
	})
}
