package snapshot

import (
	"context"
	"testing"
	"time"
)

type testSnapshotAggregate struct {
	ID            int64  `json:"id"`
	Version       uint64 `json:"version"`
	AggregateType string `json:"aggregate_type"`
}

// GetID 从存储中查询数据。
//
// 返回：
// - result：数量/计数
func (a *testSnapshotAggregate) GetID() int64 {
	return a.ID
}

// GetVersion 从存储中查询数据。
//
// 返回：
// - result：数量/计数
func (a *testSnapshotAggregate) GetVersion() uint64 {
	return a.Version
}

// GetAggregateType 从存储中查询数据。
//
// 返回：
// - result：文本结果
func (a *testSnapshotAggregate) GetAggregateType() string {
	return a.AggregateType
}

// TestSnapshotManager_EventCountStrategy 验证 SnapshotManager EventCountStrategy。
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

func TestSnapshotManager_DefaultsNonPositiveRetentionPeriod(t *testing.T) {
	ctx := context.Background()
	snapshotStore := NewMemoryStore[int64]()
	config := &Config{Frequency: 5, Enabled: true}
	mgr := NewManager[int64](snapshotStore, config)

	if mgr.config.RetentionPeriod != DefaultConfig().RetentionPeriod {
		t.Fatalf("expected default retention period, got %v", mgr.config.RetentionPeriod)
	}
	if config.RetentionPeriod != 0 {
		t.Fatalf("expected caller config to remain unchanged, got %v", config.RetentionPeriod)
	}

	err := snapshotStore.SaveSnapshot(ctx, Snapshot[int64]{
		AggregateID:   1,
		AggregateType: "test",
		Version:       1,
		Data:          []byte(`{"value":1}`),
		Timestamp:     time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}
	if err := mgr.CleanupOldSnapshots(ctx); err != nil {
		t.Fatalf("cleanup snapshots failed: %v", err)
	}
	if _, err := snapshotStore.FindSnapshot(ctx, "test", 1); err != nil {
		t.Fatalf("expected snapshot to survive default retention cleanup: %v", err)
	}
}
