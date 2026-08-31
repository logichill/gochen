package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockAggregate struct {
	id            int64
	version       uint64
	aggregateType string
}

// GetID 从存储中查询数据。
//
// 返回：
// - result：数量/计数
func (m mockAggregate) GetID() int64 { return m.id }

// GetVersion 从存储中查询数据。
//
// 返回：
// - result：数量/计数
func (m mockAggregate) GetVersion() uint64 { return m.version }

// GetAggregateType 从存储中查询数据。
//
// 返回：
// - result：文本结果
func (m mockAggregate) GetAggregateType() string { return m.aggregateType }

// TestAggregateSizeStrategy_SizeEstimator 验证 AggregateSizeStrategy SizeEstimator。
func TestAggregateSizeStrategy_SizeEstimator(t *testing.T) {
	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}
	strategy := NewAggregateSizeStrategy[int64](1000, 1024)
	strategy.SizeEstimator = func(a ISnapshotAggregate[int64]) (int, error) {
		return 2048, nil
	}

	should, err := strategy.ShouldCreateSnapshot(context.TODO(), agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatalf("expected snapshot due to size")
	}
}

func TestEventCountStrategy_ZeroFrequencyDoesNotPanic(t *testing.T) {
	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}
	strategy := &EventCountStrategy[int64]{Frequency: 0}

	should, err := strategy.ShouldCreateSnapshot(context.Background(), agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatal("expected zero frequency to disable snapshot creation")
	}
}

func TestAggregateSizeStrategy_UsesUintComparison(t *testing.T) {
	agg := mockAggregate{id: 1, version: 1<<63 + 1, aggregateType: "Test"}
	strategy := &AggregateSizeStrategy[int64]{MaxEvents: 1}

	should, err := strategy.ShouldCreateSnapshot(context.Background(), agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected large version to trigger snapshot")
	}
}

func TestTimeDurationStrategy_StoreSnapshotRefreshesStaleCache(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore[int64]()
	strategy := NewTimeDurationStrategy[int64](time.Hour, store)
	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}

	strategy.UpdateLastSnapshotTime("Test", 1, time.Now().Add(-2*time.Hour))
	if err := store.SaveSnapshot(ctx, Snapshot[int64]{
		AggregateID:   1,
		AggregateType: "Test",
		Version:       10,
		Timestamp:     time.Now(),
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	should, err := strategy.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatalf("expected fresh store snapshot to suppress creation despite stale cache")
	}
}

func TestTimeDurationStrategy_FreshCacheSkipsStoreLookup(t *testing.T) {
	strategy := NewTimeDurationStrategy[int64](time.Hour, errorSnapshotStore[int64]{})
	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}
	strategy.UpdateLastSnapshotTime("Test", 1, time.Now())

	should, err := strategy.ShouldCreateSnapshot(context.Background(), agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatal("expected fresh cache to suppress snapshot creation")
	}
}

type errorSnapshotStore[ID comparable] struct{}

func (errorSnapshotStore[ID]) SaveSnapshot(context.Context, Snapshot[ID]) error {
	return errors.New("unexpected save")
}

func (errorSnapshotStore[ID]) FindSnapshot(context.Context, string, ID) (*Snapshot[ID], error) {
	return nil, errors.New("unexpected find")
}

func (errorSnapshotStore[ID]) DeleteSnapshot(context.Context, string, ID) error {
	return errors.New("unexpected delete")
}

func (errorSnapshotStore[ID]) ListSnapshots(context.Context, string, int) ([]Snapshot[ID], error) {
	return nil, errors.New("unexpected list")
}

func (errorSnapshotStore[ID]) CleanupSnapshots(context.Context, time.Duration) error {
	return errors.New("unexpected cleanup")
}
