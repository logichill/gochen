package cached

import (
	"context"
	"testing"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/store"
	"gochen/eventing/store/decorator"
	"gochen/testkit/require"
)

func TestCachedEventStoreSeparatesTenantReads(t *testing.T) {
	for _, firstTenant := range []string{"tenant-a", ""} {
		t.Run("first_"+firstTenant, func(t *testing.T) {
			ctx := context.Background()
			base := store.NewMemoryEventStore[int64]()
			cached := NewCachedEventStore[int64](decorator.NewContextAwareEventStore[int64](base), &Config{DisableCleanup: true})
			for i, tenant := range []string{"tenant-a", "tenant-b"} {
				tenantCtx, err := contextx.WithTenantID(ctx, tenant)
				require.NoError(t, err)
				event := testutil.NewEvent[int64](1, "Order", "Created", uint64(i+1), nil)
				require.NoError(t, cached.AppendEvents(tenantCtx, "Order", 1, []eventing.IStorableEvent[int64]{event}, uint64(i)))
			}
			firstCtx, err := contextx.WithTenantID(ctx, firstTenant)
			require.NoError(t, err)
			_, err = cached.LoadEvents(firstCtx, "Order", 1, 0)
			require.NoError(t, err)
			for _, tenant := range []string{"tenant-a", "tenant-b", ""} {
				tenantCtx, err := contextx.WithTenantID(ctx, tenant)
				require.NoError(t, err)
				for range 2 {
					events, err := cached.LoadEvents(tenantCtx, "Order", 1, 0)
					require.NoError(t, err)
					if tenant == "" {
						require.Len(t, events, 2)
						continue
					}
					require.Len(t, events, 1)
					actual, _ := events[0].GetMetadata().GetString(contextx.MetadataTenantKey)
					require.Equal(t, tenant, actual)
				}
			}
		})
	}
}

func TestCachedEventStoreDoesNotBypassRequiredTenant(t *testing.T) {
	base := store.NewMemoryEventStore[int64]()
	cached := NewCachedEventStore[int64](decorator.NewTenantAwareEventStore[int64](base), &Config{DisableCleanup: true})
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)
	event := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	require.NoError(t, cached.AppendEvents(ctx, "Order", 1, []eventing.IStorableEvent[int64]{event}, 0))
	_, err = cached.LoadEvents(ctx, "Order", 1, 0)
	require.NoError(t, err)
	_, err = cached.LoadEvents(context.Background(), "Order", 1, 0)
	require.True(t, errors.Is(err, errors.InvalidInput), "cache must preserve the inner store's tenant requirement")
}

func TestCachedEventStoreInvalidatesAllTenantViews(t *testing.T) {
	ctx := context.Background()
	base := store.NewMemoryEventStore[int64]()
	cached := NewCachedEventStore[int64](decorator.NewContextAwareEventStore[int64](base), &Config{DisableCleanup: true})
	tenantCtx, err := contextx.WithTenantID(ctx, "tenant-a")
	require.NoError(t, err)
	first := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	require.NoError(t, cached.AppendEvents(tenantCtx, "Order", 1, []eventing.IStorableEvent[int64]{first}, 0))
	for _, readCtx := range []context.Context{ctx, tenantCtx} {
		_, err := cached.LoadEvents(readCtx, "Order", 1, 0)
		require.NoError(t, err)
	}
	second := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, contextx.InjectTenantID(tenantCtx, second.GetMetadata()))
	require.NoError(t, cached.AppendEvents(ctx, "Order", 1, []eventing.IStorableEvent[int64]{second}, 1))
	for _, readCtx := range []context.Context{ctx, tenantCtx} {
		events, err := cached.LoadEvents(readCtx, "Order", 1, 0)
		require.NoError(t, err)
		require.Len(t, events, 2)
	}
}

func TestCachedEventStoreInvalidatesTenantFillAfterUnscopedAppend(t *testing.T) {
	ctx := context.Background()
	tenantCtx, err := contextx.WithTenantID(ctx, "tenant-a")
	require.NoError(t, err)
	base := store.NewMemoryEventStore[int64]()
	blocking := &blockingLoadEventStore{
		IEventStreamStore: decorator.NewContextAwareEventStore[int64](base),
		loaded:            make(chan struct{}),
		release:           make(chan struct{}),
	}
	cached := NewCachedEventStore[int64](blocking, &Config{DisableCleanup: true})
	first := testutil.NewEvent[int64](1, "Order", "Created", 1, nil)
	require.NoError(t, cached.AppendEvents(tenantCtx, "Order", 1, []eventing.IStorableEvent[int64]{first}, 0))
	done := make(chan error, 1)
	go func() {
		_, err := cached.LoadEvents(tenantCtx, "Order", 1, 0)
		done <- err
	}()
	<-blocking.loaded
	second := testutil.NewEvent[int64](1, "Order", "Updated", 2, nil)
	require.NoError(t, contextx.InjectTenantID(tenantCtx, second.GetMetadata()))
	appendErr := cached.AppendEvents(ctx, "Order", 1, []eventing.IStorableEvent[int64]{second}, 1)
	close(blocking.release)
	require.NoError(t, appendErr)
	require.NoError(t, <-done)
	events, err := cached.LoadEvents(tenantCtx, "Order", 1, 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
}
