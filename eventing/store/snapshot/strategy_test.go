package snapshot

import (
	"testing"
)

type mockAggregate struct {
	id            int64
	version       uint64
	aggregateType string
}

func (m mockAggregate) GetID() int64             { return m.id }
func (m mockAggregate) GetVersion() uint64       { return m.version }
func (m mockAggregate) GetAggregateType() string { return m.aggregateType }

func TestAggregateSizeStrategy_SizeEstimator(t *testing.T) {
	agg := mockAggregate{id: 1, version: 10, aggregateType: "Test"}
	strategy := NewAggregateSizeStrategy[int64](1000, 1024)
	strategy.SizeEstimator = func(a ISnapshotAggregate[int64]) (int, error) {
		return 2048, nil
	}

	should, err := strategy.ShouldCreateSnapshot(nil, agg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatalf("expected snapshot due to size")
	}
}
