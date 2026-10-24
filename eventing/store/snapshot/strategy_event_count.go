package snapshot

import (
	"context"
)

// EventCountStrategy 基于事件数量的快照策略。
// 当事件数量达到指定频率时创建快照。
type EventCountStrategy[ID comparable] struct {
	Frequency int // 每N个事件创建一次快照
}

// NewEventCountStrategy 创建一个按事件版本频率触发的快照策略。
func NewEventCountStrategy[ID comparable](frequency int) *EventCountStrategy[ID] {
	if frequency <= 0 {
		frequency = 100 // 默认每100个事件
	}
	return &EventCountStrategy[ID]{
		Frequency: frequency,
	}
}

// ShouldCreateSnapshot 在聚合版本达到频率倍数时返回 true。
func (s *EventCountStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	if s.Frequency <= 0 {
		return false, nil
	}
	version := aggregate.GetVersion()

	// 检查版本是否是频率的倍数
	return version > 0 && version%uint64(s.Frequency) == 0, nil
}

func (s *EventCountStrategy[ID]) Name() string {
	return "EventCountStrategy"
}
