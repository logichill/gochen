package snapshot

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"gochen/errors"
	"gochen/eventing/monitoring"
	"gochen/logging"
)

// Manager 负责判断何时创建快照、保存快照以及从快照恢复聚合。
type Manager[ID comparable] struct {
	snapshotStore ISnapshotStore[ID]
	config        *Config
	strategy      ISnapshotStrategy[ID] // 快照策略
	metrics       atomic.Value          // snapshotMetricsHolder（承载 monitoring.ISnapshotMetricsRecorder），用于并发热替换且避免 data race
	mutex         sync.RWMutex
}

type snapshotMetricsHolder struct {
	rec monitoring.ISnapshotMetricsRecorder
}

// Config 快照配置，用于控制事件快照的生成和存储策略。
type Config struct {
	Frequency       int           `json:"frequency"`
	RetentionPeriod time.Duration `json:"retention_period"`
	MaxSnapshots    int           `json:"max_snapshots"`
	Enabled         bool          `json:"enabled"`
}

func DefaultConfig() *Config {
	return &Config{Frequency: 100, RetentionPeriod: 30 * 24 * time.Hour, MaxSnapshots: 10, Enabled: true}
}

// NewManager 创建一个基于给定存储与配置的快照管理器。
func NewManager[ID comparable](snapshotStore ISnapshotStore[ID], config *Config) *Manager[ID] {
	config = normalizeConfig(config)
	defaultStrategy := NewEventCountStrategy[ID](config.Frequency)
	return &Manager[ID]{snapshotStore: snapshotStore, config: config, strategy: defaultStrategy}
}

func normalizeConfig(config *Config) *Config {
	defaults := DefaultConfig()
	if config == nil {
		return defaults
	}
	normalized := *config
	if normalized.RetentionPeriod <= 0 {
		normalized.RetentionPeriod = defaults.RetentionPeriod
	}
	return &normalized
}

// SetMetricsRecorder 设置快照指标记录器（可选）。
//
// 并发语义：允许在运行时注入/替换 recorder（内部使用 atomic.Value），避免与 Record* 并发访问时产生 data race。
func (sm *Manager[ID]) SetMetricsRecorder(rec monitoring.ISnapshotMetricsRecorder) {
	// atomic.Value 不允许 Store(nil)，且要求后续 Store 的动态类型一致。
	// 用稳定的 holder 类型承载 interface，可以安全地设置 nil/不同实现。
	sm.metrics.Store(snapshotMetricsHolder{rec: rec})
}

func (sm *Manager[ID]) getMetrics() monitoring.ISnapshotMetricsRecorder {
	if sm == nil {
		return nil
	}
	v := sm.metrics.Load()
	if v == nil {
		return nil
	}
	h, ok := v.(snapshotMetricsHolder)
	if !ok {
		return nil
	}
	return h.rec
}

// SetStrategy 替换当前使用的快照触发策略。
func (sm *Manager[ID]) SetStrategy(strategy ISnapshotStrategy[ID]) {
	sm.mutex.Lock()
	sm.strategy = strategy
	sm.mutex.Unlock()
}

func (sm *Manager[ID]) Strategy() ISnapshotStrategy[ID] {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.strategy
}

// ShouldCreateSnapshot 判断当前聚合版本是否已经满足创建快照的条件。
func (sm *Manager[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	if !sm.config.Enabled || aggregate == nil {
		return false, nil
	}
	sm.mutex.RLock()
	strategy := sm.strategy
	sm.mutex.RUnlock()
	if strategy == nil {
		return false, nil
	}
	freq := sm.config.Frequency
	if freq <= 0 {
		freq = 1
	}
	should, err := strategy.ShouldCreateSnapshot(ctx, aggregate)
	if err != nil {
		return false, err
	}
	snapshot, err := sm.snapshotStore.FindSnapshot(ctx, aggregate.GetAggregateType(), aggregate.GetID())
	if err != nil {
		if aggregate.GetVersion() >= uint64(freq) {
			return true, nil
		}
		return should, nil
	}
	if aggregate.GetVersion() >= snapshot.Version+uint64(freq) {
		return true, nil
	}
	return false, nil
}

// CreateSnapshot 为指定聚合版本生成并保存一份快照。
func (sm *Manager[ID]) CreateSnapshot(ctx context.Context, aggregateID ID, aggregateType string, data any, version uint64) error {
	if !sm.config.Enabled {
		return nil
	}
	start := time.Now()
	var snapshotData any = data
	if lightweight, ok := data.(interface{ SnapshotData() any }); ok {
		snapshotData = lightweight.SnapshotData()
		snapshotLogger().Debug(ctx, "using lightweight snapshot",
			logging.Any("aggregate_id", aggregateID), logging.String("aggregate_type", aggregateType))
	}
	serializedData, err := json.Marshal(snapshotData)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to serialize snapshot data")
	}
	snap := Snapshot[ID]{AggregateID: aggregateID, AggregateType: aggregateType, Version: version, Data: serializedData, Timestamp: time.Now(), Metadata: map[string]any{"created_by": "snapshot_manager", "data_size": len(serializedData)}}
	if err := sm.snapshotStore.SaveSnapshot(ctx, snap); err != nil {
		return errors.Wrap(err, errors.Database, "failed to save snapshot").
			WithContext("aggregate_type", aggregateType).
			WithContext("aggregate_id", aggregateID)
	}
	if m := sm.getMetrics(); m != nil {
		m.RecordSnapshotCreated(time.Since(start))
	}
	snapshotLogger().Info(ctx, "snapshot created",
		logging.Any("aggregate_id", aggregateID),
		logging.Any("version", version),
		logging.Int("data_size", len(serializedData)))
	return nil
}

// LoadSnapshot 读取快照并把状态恢复到目标对象。
func (sm *Manager[ID]) LoadSnapshot(ctx context.Context, aggregateID ID, target any) (*Snapshot[ID], error) {
	start := time.Now()
	aggregateType := ""
	if typed, ok := target.(interface{ GetAggregateType() string }); ok {
		aggregateType = typed.GetAggregateType()
	}
	snapshot, err := sm.snapshotStore.FindSnapshot(ctx, aggregateType, aggregateID)
	if err != nil {
		if m := sm.getMetrics(); m != nil {
			m.RecordSnapshotLoaded(time.Since(start), false)
		}
		return nil, err
	}
	if restorer, ok := target.(interface{ RestoreFromSnapshotData(data any) error }); ok {
		var snapshotData any
		if err := json.Unmarshal(snapshot.Data, &snapshotData); err != nil {
			if m := sm.getMetrics(); m != nil {
				m.RecordSnapshotLoaded(time.Since(start), false)
			}
			return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize lightweight snapshot data").
				WithContext("aggregate_type", aggregateType).
				WithContext("aggregate_id", aggregateID)
		}
		if err := restorer.RestoreFromSnapshotData(snapshotData); err != nil {
			if m := sm.getMetrics(); m != nil {
				m.RecordSnapshotLoaded(time.Since(start), false)
			}
			return nil, errors.Wrap(err, errors.Internal, "failed to restore from lightweight snapshot").
				WithContext("aggregate_type", aggregateType).
				WithContext("aggregate_id", aggregateID)
		}
		snapshotLogger().Debug(ctx, "restored from lightweight snapshot",
			logging.Any("aggregate_id", aggregateID))
	} else {
		if err := json.Unmarshal(snapshot.Data, target); err != nil {
			if m := sm.getMetrics(); m != nil {
				m.RecordSnapshotLoaded(time.Since(start), false)
			}
			return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize snapshot data").
				WithContext("aggregate_type", aggregateType).
				WithContext("aggregate_id", aggregateID)
		}
	}
	if m := sm.getMetrics(); m != nil {
		m.RecordSnapshotLoaded(time.Since(start), true)
	}
	snapshotLogger().Debug(ctx, "snapshot loaded",
		logging.Any("aggregate_id", aggregateID),
		logging.Any("version", snapshot.Version))
	return snapshot, nil
}

// CleanupOldSnapshots 根据保留期删除过旧快照。
func (sm *Manager[ID]) CleanupOldSnapshots(ctx context.Context) error {
	return sm.snapshotStore.CleanupSnapshots(ctx, sm.config.RetentionPeriod)
}

// SnapshotStats 汇总当前快照存储中的数量、类型分布和策略配置。
func (sm *Manager[ID]) SnapshotStats(ctx context.Context) (monitoring.SnapshotManagerStats, error) {
	snapshots, err := sm.snapshotStore.ListSnapshots(ctx, "", 0)
	if err != nil {
		return monitoring.SnapshotManagerStats{}, err
	}
	stats := monitoring.SnapshotManagerStats{
		TotalSnapshots: len(snapshots),
		Enabled:        sm.config.Enabled,
		Frequency:      sm.config.Frequency,
		RetentionDays:  sm.config.RetentionPeriod.Hours() / 24,
		Strategy:       "",
		ByType:         make(map[string]int),
	}
	if strategy := sm.Strategy(); strategy != nil {
		stats.Strategy = strategy.Name()
	}
	for _, s := range snapshots {
		stats.ByType[s.AggregateType]++
	}
	return stats, nil
}
