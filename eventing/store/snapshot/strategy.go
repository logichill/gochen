package snapshot

import (
	"context"
	"sync"
	"time"
)

// ISnapshotAggregate 快照聚合接口
// 避免循环依赖，定义快照所需的最小接口
type ISnapshotAggregate[ID comparable] interface {
	GetID() ID
	GetVersion() uint64
	GetAggregateType() string
}

// ISnapshotStrategy 快照策略接口
// 用于判断是否应该为聚合根创建快照
type ISnapshotStrategy[ID comparable] interface {
	ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error)
	GetName() string // 策略名称
}

// EventCountStrategy 基于事件数量的快照策略
// 当事件数量达到指定频率时创建快照
type EventCountStrategy[ID comparable] struct {
	Frequency int // 每N个事件创建一次快照
}

// NewEventCountStrategy 创建事件计数策略
func NewEventCountStrategy[ID comparable](frequency int) *EventCountStrategy[ID] {
	if frequency <= 0 {
		frequency = 100 // 默认每100个事件
	}
	return &EventCountStrategy[ID]{
		Frequency: frequency,
	}
}

// ShouldCreateSnapshot 判断是否应该创建快照
func (s *EventCountStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	version := aggregate.GetVersion()

	// 检查版本是否是频率的倍数
	return version > 0 && version%uint64(s.Frequency) == 0, nil
}

// GetName 获取策略名称
func (s *EventCountStrategy[ID]) GetName() string {
	return "EventCountStrategy"
}

// TimeDurationStrategy 基于时间间隔的快照策略
// 当距离上次快照超过指定时间时创建快照
type TimeDurationStrategy[ID comparable] struct {
	Duration          time.Duration
	lastSnapshotTimes map[string]time.Time // 聚合键 -> 上次快照时间
	snapshotStore     ISnapshotStore[ID]
	mutex             sync.RWMutex
}

// NewTimeDurationStrategy 创建时间间隔策略
func NewTimeDurationStrategy[ID comparable](duration time.Duration, snapshotStore ISnapshotStore[ID]) *TimeDurationStrategy[ID] {
	if duration <= 0 {
		duration = 24 * time.Hour // 默认24小时
	}
	return &TimeDurationStrategy[ID]{
		Duration:          duration,
		lastSnapshotTimes: make(map[string]time.Time),
		snapshotStore:     snapshotStore,
	}
}

// ShouldCreateSnapshot 判断是否应该创建快照
func (s *TimeDurationStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	aggregateID := aggregate.GetID()
	aggregateType := aggregate.GetAggregateType()
	key := snapshotKey(aggregateType, aggregateID)

	// 尝试从快照存储获取最后快照时间
	s.mutex.Lock()
	if snapshot, err := s.snapshotStore.GetSnapshot(ctx, aggregateType, aggregateID); err == nil {
		s.lastSnapshotTimes[key] = snapshot.Timestamp
	}

	// 检查是否有记录
	lastTime, exists := s.lastSnapshotTimes[key]
	s.mutex.Unlock()
	if !exists {
		// 没有快照记录，应该创建
		return true, nil
	}

	// 检查是否超过时间间隔
	return time.Since(lastTime) >= s.Duration, nil
}

// GetName 获取策略名称
func (s *TimeDurationStrategy[ID]) GetName() string {
	return "TimeDurationStrategy"
}

// UpdateLastSnapshotTime 更新最后快照时间
func (s *TimeDurationStrategy[ID]) UpdateLastSnapshotTime(aggregateType string, aggregateID ID, timestamp time.Time) {
	s.mutex.Lock()
	key := snapshotKey(aggregateType, aggregateID)
	s.lastSnapshotTimes[key] = timestamp
	s.mutex.Unlock()
}

// AggregateSizeStrategy 基于聚合大小的快照策略
// 当聚合事件数量或数据大小超过阈值时创建快照
type AggregateSizeStrategy[ID comparable] struct {
	MaxEvents    int // 最大事件数量
	MaxSizeBytes int // 最大数据大小（字节）

	// SizeEstimator 可选：估算聚合大小的函数
	// 返回聚合占用的字节数，用于判断是否超过 MaxSizeBytes。
	// 若未提供，则仅基于事件数量判断。
	SizeEstimator func(aggregate ISnapshotAggregate[ID]) (int, error)
}

// NewAggregateSizeStrategy 创建聚合大小策略
func NewAggregateSizeStrategy[ID comparable](maxEvents int, maxSizeBytes int) *AggregateSizeStrategy[ID] {
	if maxEvents <= 0 {
		maxEvents = 1000 // 默认1000个事件
	}
	if maxSizeBytes <= 0 {
		maxSizeBytes = 1024 * 1024 // 默认1MB
	}
	return &AggregateSizeStrategy[ID]{
		MaxEvents:    maxEvents,
		MaxSizeBytes: maxSizeBytes,
	}
}

// ShouldCreateSnapshot 判断是否应该创建快照
func (s *AggregateSizeStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	version := aggregate.GetVersion()

	// 检查事件数量是否超过阈值
	if int(version) >= s.MaxEvents {
		return true, nil
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

// GetName 获取策略名称
func (s *AggregateSizeStrategy[ID]) GetName() string {
	return "AggregateSizeStrategy"
}

// CompositeSnapshotStrategy 组合快照策略
// 支持多个策略组合，任一策略满足条件即创建快照
type CompositeSnapshotStrategy[ID comparable] struct {
	strategies []ISnapshotStrategy[ID]
	mode       CompositeMode
}

// CompositeMode 组合模式
type CompositeMode string

const (
	// CompositeModeAny 任一策略满足即创建快照
	CompositeModeAny CompositeMode = "any"
	// CompositeModeAll 所有策略都满足才创建快照
	CompositeModeAll CompositeMode = "all"
)

// NewCompositeSnapshotStrategy 创建组合策略
func NewCompositeSnapshotStrategy[ID comparable](mode CompositeMode, strategies ...ISnapshotStrategy[ID]) *CompositeSnapshotStrategy[ID] {
	if mode == "" {
		mode = CompositeModeAny // 默认使用ANY模式
	}
	return &CompositeSnapshotStrategy[ID]{
		strategies: strategies,
		mode:       mode,
	}
}

// AddStrategy 添加策略
func (s *CompositeSnapshotStrategy[ID]) AddStrategy(strategy ISnapshotStrategy[ID]) {
	s.strategies = append(s.strategies, strategy)
}

// ShouldCreateSnapshot 判断是否应该创建快照
func (s *CompositeSnapshotStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	if len(s.strategies) == 0 {
		return false, nil
	}

	switch s.mode {
	case CompositeModeAny:
		// ANY模式：任一策略满足即返回true
		for _, strategy := range s.strategies {
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
		for _, strategy := range s.strategies {
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

// GetName 获取策略名称
func (s *CompositeSnapshotStrategy[ID]) GetName() string {
	return "CompositeSnapshotStrategy"
}

// GetStrategies 获取所有子策略
func (s *CompositeSnapshotStrategy[ID]) GetStrategies() []ISnapshotStrategy[ID] {
	return s.strategies
}

// GetMode 获取组合模式
func (s *CompositeSnapshotStrategy[ID]) GetMode() CompositeMode {
	return s.mode
}

// PresetSnapshotStrategies 预设快照策略
type PresetSnapshotStrategies[ID comparable] struct{}

// DefaultStrategy 默认策略（每100个事件）
func (PresetSnapshotStrategies[ID]) DefaultStrategy() ISnapshotStrategy[ID] {
	return NewEventCountStrategy[ID](100)
}

// AggressiveStrategy 激进策略（每50个事件或12小时）
func (PresetSnapshotStrategies[ID]) AggressiveStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAny,
		NewEventCountStrategy[ID](50),
		NewTimeDurationStrategy[ID](12*time.Hour, snapshotStore),
	)
}

// ConservativeStrategy 保守策略（每200个事件且至少48小时）
func (PresetSnapshotStrategies[ID]) ConservativeStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAll,
		NewEventCountStrategy[ID](200),
		NewTimeDurationStrategy[ID](48*time.Hour, snapshotStore),
	)
}

// HighVolumeStrategy 高频策略（适合高并发聚合）
func (PresetSnapshotStrategies[ID]) HighVolumeStrategy(snapshotStore ISnapshotStore[ID]) ISnapshotStrategy[ID] {
	return NewCompositeSnapshotStrategy[ID](
		CompositeModeAny,
		NewEventCountStrategy[ID](20),
		NewAggregateSizeStrategy[ID](500, 512*1024), // 500事件或512KB
		NewTimeDurationStrategy[ID](6*time.Hour, snapshotStore),
	)
}
