package snapshot

import (
	"context"

	"gochen/errors"
)

// AggregateSizeStrategy 基于聚合大小的快照策略。
// 当聚合事件数量或数据大小超过阈值时创建快照。
type AggregateSizeStrategy[ID comparable] struct {
	MaxEvents    int // 最大事件数量
	MaxSizeBytes int // 最大数据大小（字节）

	// SizeEstimator 可选：估算聚合大小的函数
	// 返回聚合占用的字节数，用于判断是否超过 MaxSizeBytes。
	// 若未提供，则仅基于事件数量判断。
	SizeEstimator func(aggregate ISnapshotAggregate[ID]) (int, error)

	// SnapshotStore 可选：提供后按“当前版本距上次快照的增量”判断，
	// 避免长寿命聚合跨过阈值后每次追加都触发快照（快照风暴）。
	SnapshotStore ISnapshotStore[ID]
}

const defaultAggregateSizeMaxEvents = 1000

// NewAggregateSizeStrategy 创建一个按聚合大小阈值触发的快照策略。
//
// snapshotStore 可选（变参以兼容旧调用）：提供后按“当前版本距上次快照的
// 增量”判断，避免长寿命聚合跨过阈值后每次追加都触发快照（快照风暴）；
// 未提供时退化为绝对版本号阈值比较。
func NewAggregateSizeStrategy[ID comparable](maxEvents int, maxSizeBytes int, snapshotStore ...ISnapshotStore[ID]) *AggregateSizeStrategy[ID] {
	if maxEvents <= 0 {
		maxEvents = defaultAggregateSizeMaxEvents // 默认1000个事件
	}
	if maxSizeBytes <= 0 {
		maxSizeBytes = 1024 * 1024 // 默认1MB
	}
	strategy := &AggregateSizeStrategy[ID]{
		MaxEvents:    maxEvents,
		MaxSizeBytes: maxSizeBytes,
	}
	if len(snapshotStore) > 0 {
		strategy.SnapshotStore = snapshotStore[0]
	}
	return strategy
}

// SetSnapshotStore 注入快照存储，用于把判定基准从绝对版本号切换为版本增量。
func (s *AggregateSizeStrategy[ID]) SetSnapshotStore(store ISnapshotStore[ID]) {
	if s == nil {
		return
	}
	s.SnapshotStore = store
}

// ShouldCreateSnapshot 在事件数或估算大小超过阈值时返回 true。
//
// 提供 SnapshotStore 时基于距上次快照的版本增量判断（增量 ≥ MaxEvents 触发）；
// 未提供时回退到绝对版本号阈值判断（历史行为）。
func (s *AggregateSizeStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	version := aggregate.GetVersion()

	// 检查事件数量是否超过阈值
	if s.MaxEvents > 0 {
		threshold := uint64(s.MaxEvents)
		if s.SnapshotStore != nil {
			snapshot, err := s.SnapshotStore.FindSnapshot(ctx, aggregate.GetAggregateType(), aggregate.GetID())
			if err != nil && !errors.Is(err, errors.NotFound) {
				return false, errors.Wrap(err, errors.Database, "failed to query last snapshot for size strategy").
					WithContext("aggregate_type", aggregate.GetAggregateType()).
					WithContext("aggregate_id", aggregate.GetID())
			}
			if snapshot != nil {
				if snapshot.Version < version && version-snapshot.Version >= threshold {
					return true, nil
				}
			} else if version >= threshold {
				return true, nil
			}
		} else if version >= threshold {
			return true, nil
		}
	}

	// 检查数据大小是否超过阈值（若提供估算器）
	if s.SizeEstimator != nil && s.MaxSizeBytes > 0 {
		size, err := s.SizeEstimator(aggregate)
		if err != nil {
			return false, err
		}
		if size >= s.MaxSizeBytes {
			return true, nil
		}
	}

	return false, nil
}

func (s *AggregateSizeStrategy[ID]) Name() string {
	return "AggregateSizeStrategy"
}
