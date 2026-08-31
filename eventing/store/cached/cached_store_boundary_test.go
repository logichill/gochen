package cached

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/clock"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/monitoring"
	"gochen/eventing/store"
)

func TestCachedEventStoreContextCancellationDoesNotUseCache(t *testing.T) {
	ctx := context.Background()
	inner := store.NewMemoryEventStore[int64]()
	cached := NewCachedEventStore[int64](inner, &Config{DisableCleanup: true})
	defer cached.Close()

	event := makeTestEvent(1, "Created", 1)
	require.NoError(t, cached.AppendEvents(ctx, "TestAggregate", 1, toStorableEvents([]eventing.Event[int64]{event}), 0))
	_, err := cached.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = cached.LoadEvents(canceled, "TestAggregate", 1, 0)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCachedEventStoreRejectsNilInnerStoreWithoutPanicking(t *testing.T) {
	var typedNil *store.MemoryEventStore[int64]
	for _, inner := range []store.IEventStreamStore[int64]{nil, typedNil} {
		cached := NewCachedEventStore[int64](inner, &Config{DisableCleanup: true})
		_, err := cached.LoadEvents(context.Background(), "TestAggregate", 1, 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
		require.NoError(t, cached.Close())
	}
}

func TestCachedEventStoreUsesRealClockForTypedNilClock(t *testing.T) {
	var typedNil *clock.ManualClock
	cached := NewCachedEventStore[int64](store.NewMemoryEventStore[int64](), &Config{
		Clock:          typedNil,
		DisableCleanup: true,
	})
	defer cached.Close()

	require.NoError(t, cached.AppendEvents(
		context.Background(),
		"TestAggregate",
		1,
		toStorableEvents([]eventing.Event[int64]{makeTestEvent(1, "Created", 1)}),
		0,
	))
	_, err := cached.LoadEvents(context.Background(), "TestAggregate", 1, 0)
	require.NoError(t, err)
}

func TestCachedEventStoreNilReceiverHelpersAreSafe(t *testing.T) {
	var cached *CachedEventStore[int64]
	cached.SetMetricsRecorder(nil)
	cached.ClearCache()
	require.NoError(t, cached.Close())
	require.Equal(t, 0.0, cached.GetHitRate())
	require.Equal(t, &CacheStats{}, cached.Stats())
	require.Equal(t, monitoring.CacheStats{}, cached.CacheStats())
}
