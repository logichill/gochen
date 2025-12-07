package snapshot

import (
	"context"
	"testing"
)

type testSnapshotAggregate struct {
	ID            int64  `json:"id"`
	Version       uint64 `json:"version"`
	AggregateType string `json:"aggregate_type"`
}

func (a *testSnapshotAggregate) GetID() int64 {
	return a.ID
}

func (a *testSnapshotAggregate) GetVersion() uint64 {
	return a.Version
}

func (a *testSnapshotAggregate) GetAggregateType() string {
	return a.AggregateType
}

func TestSnapshotManager_EventCountStrategy(t *testing.T) {
	ctx := context.Background()
	snapshotStore := NewMemoryStore[int64]()
	config := &Config{Frequency: 5, Enabled: true}
	mgr := NewManager[int64](snapshotStore, config)

	agg := &testSnapshotAggregate{ID: 1, Version: 5, AggregateType: "test"}

	should, err := mgr.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if !should {
		t.Fatalf("expected snapshot creation at version 5")
	}

	if err := mgr.CreateSnapshot(ctx, agg.ID, agg.AggregateType, agg, agg.Version); err != nil {
		t.Fatalf("create snapshot failed: %v", err)
	}

	// 同一版本不应重复创建快照
	should, err = mgr.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatalf("expected no snapshot creation for existing version")
	}

	// 当版本达到下一个频率倍数时应创建快照
	agg.Version = 10
	should, err = mgr.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatalf("expected snapshot creation at version 10")
	}

	// 非倍数版本不应创建
	agg.Version = 7
	should, err = mgr.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatalf("expected no snapshot creation at version 7")
	}
}
