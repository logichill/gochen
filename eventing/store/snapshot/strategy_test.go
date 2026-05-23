package snapshot

import (
	"context"
	"testing"
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
