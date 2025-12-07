package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gochen/eventing/monitoring"
	"gochen/logging"
)

// Manager 快照管理器
type Manager[ID comparable] struct {
	snapshotStore ISnapshotStore[ID]
	config        *Config
	strategy      ISnapshotStrategy[ID] // 快照策略
	mutex         sync.RWMutex
}

// Config 快照配置
type Config struct {
	Frequency       int           `json:"frequency"`
	RetentionPeriod time.Duration `json:"retention_period"`
	MaxSnapshots    int           `json:"max_snapshots"`
	Enabled         bool          `json:"enabled"`
}

// DefaultConfig 默认快照配置
func DefaultConfig() *Config {
	return &Config{Frequency: 100, RetentionPeriod: 30 * 24 * time.Hour, MaxSnapshots: 10, Enabled: true}
}

// NewManager 创建快照管理器
func NewManager[ID comparable](snapshotStore ISnapshotStore[ID], config *Config) *Manager[ID] {
	if config == nil {
		config = DefaultConfig()
	}
	defaultStrategy := NewEventCountStrategy[ID](config.Frequency)
	return &Manager[ID]{snapshotStore: snapshotStore, config: config, strategy: defaultStrategy}
}

// SetStrategy 设置快照策略
func (sm *Manager[ID]) SetStrategy(strategy ISnapshotStrategy[ID]) {
	sm.mutex.Lock()
	sm.strategy = strategy
	sm.mutex.Unlock()
}

// GetStrategy 获取当前快照策略
func (sm *Manager[ID]) GetStrategy() ISnapshotStrategy[ID] {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.strategy
}

// ShouldCreateSnapshot 判断是否应该创建快照
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
	snapshot, err := sm.snapshotStore.GetSnapshot(ctx, aggregate.GetAggregateType(), aggregate.GetID())
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

// CreateSnapshot 创建快照
func (sm *Manager[ID]) CreateSnapshot(ctx context.Context, aggregateID ID, aggregateType string, data any, version uint64) error {
	if !sm.config.Enabled {
		return nil
	}
	start := time.Now()
	var snapshotData any = data
	if lightweight, ok := data.(interface{ GetSnapshotData() any }); ok {
		snapshotData = lightweight.GetSnapshotData()
		snapshotLogger().Debug(ctx, "[SnapshotManager] 使用轻量快照",
			logging.Any("aggregate_id", aggregateID), logging.String("aggregate_type", aggregateType))
	}
	serializedData, err := json.Marshal(snapshotData)
	if err != nil {
		return fmt.Errorf("failed to serialize snapshot data: %w", err)
	}
	snap := Snapshot[ID]{AggregateID: aggregateID, AggregateType: aggregateType, Version: version, Data: serializedData, Timestamp: time.Now(), Metadata: map[string]any{"created_by": "snapshot_manager", "data_size": len(serializedData)}}
	if err := sm.snapshotStore.SaveSnapshot(ctx, snap); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}
	monitoring.GlobalMetrics().RecordSnapshotCreated(time.Since(start))
	snapshotLogger().Info(ctx, "[SnapshotManager] 创建快照成功",
		logging.Any("aggregate_id", aggregateID),
		logging.Any("version", version),
		logging.Int("data_size", len(serializedData)))
	return nil
}

// LoadSnapshot 加载快照
func (sm *Manager[ID]) LoadSnapshot(ctx context.Context, aggregateID ID, target any) (*Snapshot[ID], error) {
	start := time.Now()
	aggregateType := ""
	if typed, ok := target.(interface{ GetAggregateType() string }); ok {
		aggregateType = typed.GetAggregateType()
	}
	snapshot, err := sm.snapshotStore.GetSnapshot(ctx, aggregateType, aggregateID)
	if err != nil {
		monitoring.GlobalMetrics().RecordSnapshotLoaded(time.Since(start), false)
		return nil, err
	}
	if restorer, ok := target.(interface{ RestoreFromSnapshotData(data any) error }); ok {
		var snapshotData any
		if err := json.Unmarshal(snapshot.Data, &snapshotData); err != nil {
			monitoring.GlobalMetrics().RecordSnapshotLoaded(time.Since(start), false)
			return nil, fmt.Errorf("failed to deserialize lightweight snapshot data: %w", err)
		}
		if err := restorer.RestoreFromSnapshotData(snapshotData); err != nil {
			monitoring.GlobalMetrics().RecordSnapshotLoaded(time.Since(start), false)
			return nil, fmt.Errorf("failed to restore from lightweight snapshot: %w", err)
		}
		snapshotLogger().Debug(ctx, "[SnapshotManager] 使用轻量快照恢复",
			logging.Any("aggregate_id", aggregateID))
	} else {
		if err := json.Unmarshal(snapshot.Data, target); err != nil {
			monitoring.GlobalMetrics().RecordSnapshotLoaded(time.Since(start), false)
			return nil, fmt.Errorf("failed to deserialize snapshot data: %w", err)
		}
	}
	monitoring.GlobalMetrics().RecordSnapshotLoaded(time.Since(start), true)
	snapshotLogger().Debug(ctx, "[SnapshotManager] 加载快照成功",
		logging.Any("aggregate_id", aggregateID),
		logging.Any("version", snapshot.Version))
	return snapshot, nil
}

// CleanupOldSnapshots 清理旧快照
func (sm *Manager[ID]) CleanupOldSnapshots(ctx context.Context) error {
	return sm.snapshotStore.CleanupSnapshots(ctx, sm.config.RetentionPeriod)
}

// GetSnapshotStats 获取快照统计信息
func (sm *Manager[ID]) GetSnapshotStats(ctx context.Context) (map[string]any, error) {
	snapshots, err := sm.snapshotStore.GetSnapshots(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	stats := map[string]any{"total_snapshots": len(snapshots), "enabled": sm.config.Enabled, "frequency": sm.config.Frequency, "retention_days": sm.config.RetentionPeriod.Hours() / 24, "strategy": sm.strategy.GetName()}
	typeStats := make(map[string]int)
	for _, s := range snapshots {
		typeStats[s.AggregateType]++
	}
	stats["by_type"] = typeStats
	return stats, nil
}
