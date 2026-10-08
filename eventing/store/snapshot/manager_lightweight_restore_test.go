package snapshot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gochen/codec/jsoncodec"
)

type testLightweightAggregate struct {
	ID            int64
	Version       uint64
	AggregateType string
	Value         int64
}

func (a *testLightweightAggregate) GetID() int64             { return a.ID }
func (a *testLightweightAggregate) GetVersion() uint64       { return a.Version }
func (a *testLightweightAggregate) GetAggregateType() string { return a.AggregateType }
func (a *testLightweightAggregate) SnapshotData() any        { return map[string]any{"value": a.Value} }
func (a *testLightweightAggregate) RestoreFromSnapshotData(data any) error {
	var state struct {
		Value int64 `json:"value"`
	}
	if err := jsoncodec.TranscodeInto(data, &state); err != nil {
		return err
	}
	a.Value = state.Value
	return nil
}

func TestSnapshotManager_CreateAndLoadSnapshot_Lightweight(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := NewMemoryStore[int64]()
	mgr := NewManager[int64](store, &Config{Frequency: 1, Enabled: true})

	agg := &testLightweightAggregate{ID: 1, Version: 10, AggregateType: "test", Value: 9007199254740993}
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
	if target.Value != 9007199254740993 {
		t.Fatalf("expected restored value=9007199254740993, got %d", target.Value)
	}
}

// 默认恢复路径也必须保留动态字段中的大整数。
func TestSnapshotManager_DefaultRestorePreservesDynamicInteger(t *testing.T) {
	mgr := NewManager[int64](NewMemoryStore[int64](), &Config{Frequency: 1, Enabled: true})
	source := &testDynamicSnapshot{Value: int64(9007199254740993)}
	if err := mgr.CreateSnapshot(t.Context(), 1, source.GetAggregateType(), source, 1); err != nil {
		t.Fatal(err)
	}
	target := &testDynamicSnapshot{}
	if _, err := mgr.LoadSnapshot(t.Context(), 1, target); err != nil {
		t.Fatal(err)
	}
	number, ok := target.Value.(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("dynamic integer changed: %#v", target.Value)
	}
}

type testDynamicSnapshot struct {
	Value any `json:"value"`
}

func (*testDynamicSnapshot) GetAggregateType() string { return "dynamic" }
