package snapshot

import (
	"context"
	"testing"
	"time"
)

type testLightweightAggregate struct {
	ID            int64
	Version       uint64
	AggregateType string
	Value         int
}

func (a *testLightweightAggregate) GetID() int64             { return a.ID }
func (a *testLightweightAggregate) GetVersion() uint64       { return a.Version }
func (a *testLightweightAggregate) GetAggregateType() string { return a.AggregateType }
func (a *testLightweightAggregate) SnapshotData() any        { return map[string]any{"value": a.Value} }
func (a *testLightweightAggregate) RestoreFromSnapshotData(d any) error {
	m, ok := d.(map[string]any)
	if !ok {
		return nil
	}
	v, _ := m["value"].(float64) // json.Unmarshal 会把 number 解成 float64
	a.Value = int(v)
	return nil
}

func TestSnapshotManager_CreateAndLoadSnapshot_Lightweight(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := NewMemoryStore[int64]()
	mgr := NewManager[int64](store, &Config{Frequency: 1, Enabled: true})

	agg := &testLightweightAggregate{ID: 1, Version: 10, AggregateType: "test", Value: 42}
	if err := mgr.CreateSnapshot(ctx, agg.ID, agg.AggregateType, agg, agg.Version); err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	// 使用 lightweight restore 路径
	target := &testLightweightAggregate{ID: 1, AggregateType: "test"}
	snap, err := mgr.LoadSnapshot(ctx, 1, target)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}
	if snap == nil {
		t.Fatalf("expected snapshot")
	}
	if target.Value != 42 {
		t.Fatalf("expected restored value=42, got %d", target.Value)
	}
}
