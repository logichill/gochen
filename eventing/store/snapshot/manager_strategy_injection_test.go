package snapshot

import (
	"context"
	"testing"
	"time"
)

// fixedBoolStrategy 是只有固定裁决结果的测试策略，用于验证「策略是唯一裁决者」。
type fixedBoolStrategy struct {
	should bool
}

func (s fixedBoolStrategy) ShouldCreateSnapshot(context.Context, ISnapshotAggregate[int64]) (bool, error) {
	return s.should, nil
}

func (s fixedBoolStrategy) Name() string { return "fixedBoolStrategy" }

// 策略返回 false 时不得被 config.Frequency 兜底覆盖。
func TestShouldCreateSnapshotStrategyFalseIsNotOverriddenByFrequency(t *testing.T) {
	store := NewMemoryStore[int64]()
	manager := NewManager[int64](store, &Config{Enabled: true, Frequency: 1})
	manager.SetStrategy(fixedBoolStrategy{should: false})

	should, err := manager.ShouldCreateSnapshot(context.Background(), mockAggregate{id: 1, version: 100, aggregateType: "Test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatal("strategy said no; manager must not fall back to config.Frequency")
	}
}

// 策略返回 true 且版本未快照过时必须允许创建。
func TestShouldCreateSnapshotStrategyTrueCreatesOncePerVersion(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore[int64]()
	manager := NewManager[int64](store, &Config{Enabled: true, Frequency: 1})
	manager.SetStrategy(fixedBoolStrategy{should: true})

	agg := mockAggregate{id: 1, version: 5, aggregateType: "Test"}
	should, err := manager.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected snapshot when strategy approves and no snapshot exists")
	}
	if err := manager.CreateSnapshot(ctx, agg.GetID(), agg.GetAggregateType(), map[string]any{"v": 1}, agg.GetVersion()); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	agg.version = 10
	should, err = manager.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected new snapshot for newer version")
	}
}

// SetStrategy 必须自动注入 SnapshotStore，使大小策略按版本增量判断。
func TestSetStrategyInjectsSnapshotStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore[int64]()
	manager := NewManager[int64](store, &Config{Enabled: true, Frequency: 100})

	size := NewAggregateSizeStrategy[int64](10, 1024)
	manager.SetStrategy(size)
	if size.SnapshotStore == nil {
		t.Fatal("SetStrategy must inject manager snapshot store into size strategy")
	}

	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}
	should, err := manager.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected first snapshot at threshold")
	}
	if err := manager.CreateSnapshot(ctx, agg.GetID(), agg.GetAggregateType(), map[string]any{"v": 1}, agg.GetVersion()); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	// 刚建过快照的版本：即使绝对版本号仍 >= 阈值，也不应重复建快照。
	should, err = manager.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatal("size strategy must use version delta, not absolute version")
	}
	agg.version = 20
	should, err = manager.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected snapshot after delta reaches threshold")
	}
}

// 组合策略中的时间子策略必须收到快照创建通知，否则窗口内会重复判定。
func TestCreateSnapshotNotifiesNestedTimeStrategy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore[int64]()
	manager := NewManager[int64](store, &Config{Enabled: true, Frequency: 100})

	timeStrategy := NewTimeDurationStrategy[int64](time.Hour, store)
	composite := NewCompositeSnapshotStrategy[int64](CompositeModeAny, timeStrategy)
	manager.SetStrategy(composite)

	agg := mockAggregate{id: 7, version: 1, aggregateType: "Test"}
	if err := manager.CreateSnapshot(ctx, agg.GetID(), agg.GetAggregateType(), map[string]any{"v": 1}, agg.GetVersion()); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	should, err := timeStrategy.ShouldCreateSnapshot(ctx, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if should {
		t.Fatal("time strategy must see the freshly created snapshot and stay quiet")
	}
}
