package snapshot

import (
	"context"
	"sync"
)

// CompositeSnapshotStrategy 组合快照策略。
// 支持多个策略组合，任一策略满足条件即创建快照。
type CompositeSnapshotStrategy[ID comparable] struct {
	mu         sync.RWMutex
	strategies []ISnapshotStrategy[ID]
	mode       CompositeMode
}

// CompositeMode 组合模式。
type CompositeMode string

const (
	// CompositeModeAny 任一策略满足即创建快照
	CompositeModeAny CompositeMode = "any"
	// CompositeModeAll 所有策略都满足才创建快照
	CompositeModeAll CompositeMode = "all"
)

// NewCompositeSnapshotStrategy 创建一个组合多个子策略的快照策略。
func NewCompositeSnapshotStrategy[ID comparable](mode CompositeMode, strategies ...ISnapshotStrategy[ID]) *CompositeSnapshotStrategy[ID] {
	if mode == "" {
		mode = CompositeModeAny // 默认使用ANY模式
	}
	return &CompositeSnapshotStrategy[ID]{
		strategies: strategies,
		mode:       mode,
	}
}

// AddStrategy 追加一个子策略。
//
// 通过互斥锁保护：复合策略可能在运行期被动态扩展，
// 与并发执行的 ShouldCreateSnapshot 遍历必须互斥。
func (s *CompositeSnapshotStrategy[ID]) AddStrategy(strategy ISnapshotStrategy[ID]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strategies = append(s.strategies, strategy)
}

// ShouldCreateSnapshot 按组合模式聚合各子策略的判断结果。
func (s *CompositeSnapshotStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	s.mu.RLock()
	// 元素级快照：仅拷贝切片头无法隔离 AddStrategy 同时触发的扩容/重排，
	// 快照必须复制底层数组。
	strategies := append([]ISnapshotStrategy[ID](nil), s.strategies...)
	s.mu.RUnlock()
	if len(strategies) == 0 {
		return false, nil
	}

	switch s.mode {
	case CompositeModeAny:
		// ANY模式：任一策略满足即返回true
		for _, strategy := range strategies {
			should, err := strategy.ShouldCreateSnapshot(ctx, aggregate)
			if err != nil {
				return false, err
			}
			if should {
				return true, nil
			}
		}
		return false, nil

	case CompositeModeAll:
		// ALL模式：所有策略都满足才返回true
		for _, strategy := range strategies {
			should, err := strategy.ShouldCreateSnapshot(ctx, aggregate)
			if err != nil {
				return false, err
			}
			if !should {
				return false, nil
			}
		}
		return true, nil

	default:
		return false, nil
	}
}

func (s *CompositeSnapshotStrategy[ID]) Name() string {
	return "CompositeSnapshotStrategy"
}

func (s *CompositeSnapshotStrategy[ID]) Strategies() []ISnapshotStrategy[ID] {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ISnapshotStrategy[ID](nil), s.strategies...)
}

func (s *CompositeSnapshotStrategy[ID]) Mode() CompositeMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}
