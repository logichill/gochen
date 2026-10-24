package snapshot

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"gochen/codec/jsoncodec"
	"gochen/errors"
	"gochen/eventing/monitoring"
	"gochen/observe/logging"
)

// ShouldCreateSnapshot 判断当前聚合版本是否已经满足创建快照的条件。
//
// 策略是唯一裁决者：策略返回 false 时不得用 config.Frequency 兜底覆盖，
// 否则任何自定义策略（时间/大小/组合）都会被绝对版本号阈值架空。
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
	should, err := strategy.ShouldCreateSnapshot(ctx, aggregate)
	if err != nil {
		return false, err
	}
	if !should {
		return false, nil
	}
	snapshot, err := sm.snapshotStore.FindSnapshot(ctx, aggregate.GetAggregateType(), aggregate.GetID())
	// 非 NotFound 的存储故障必须向上传播，否则会对宕机存储盲目重试写入。
	if err != nil && !errors.Is(err, errors.NotFound) {
		return false, errors.Wrap(err, errors.Database, "failed to query existing snapshot").
			WithContext("aggregate_type", aggregate.GetAggregateType()).
			WithContext("aggregate_id", aggregate.GetID())
	}
	// 策略裁决优先；仅在“当前版本尚未建过快照”时生效，避免已建快照的版本重复建快照。
	return snapshot == nil || snapshot.Version < aggregate.GetVersion(), nil
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
	// 联动时间策略的内存缓存：否则 TimeDurationStrategy 在缓存窗口内
	// 仍认为“距上次快照未超时”，导致同一聚合在窗口内被反复判定需要建快照。
	sm.notifySnapshotCreated(aggregateType, aggregateID, snap.Timestamp)
	snapshotLogger().Info(ctx, "snapshot created",
		logging.Any("aggregate_id", aggregateID),
		logging.Any("version", version),
		logging.Int("data_size", len(serializedData)))
	return nil
}

// LoadSnapshot 读取快照并把状态恢复到目标对象。
// 动态字段中的 JSON 数值保留为 json.Number，避免大整数标识符丢失精度。
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
		if err := jsoncodec.DecodeInto(snapshot.Data, &snapshotData); err != nil {
			if m := sm.getMetrics(); m != nil {
				m.RecordSnapshotLoaded(time.Since(start), false)
			}
			return nil, errors.Wrap(err, errors.InvalidInput, "failed to deserialize lightweight snapshot data").
				WithContext("aggregate_type", aggregateType).
				WithContext("aggregate_id", aggregateID)
		}

		// 轻量快照双缓冲回退：RestoreFromSnapshotData 是目标实现的定制逻辑，
		// 可能在失败前已就地修改部分字段。恢复前先序列化 target 当前状态，
		// 失败时把原状态解码回写，保证目标不被半_restore 污染（调用方的
		// 全量重放回退不会落在脏状态上）。
		rollbackData, rollbackErr := jsoncodec.Encode(target)
		if err := restorer.RestoreFromSnapshotData(snapshotData); err != nil {
			if rollbackErr == nil {
				if restoreErr := jsoncodec.DecodeIntoValue(rollbackData, target); restoreErr != nil {
					snapshotLogger().Error(ctx, "snapshot restore rollback failed",
						logging.Any("aggregate_id", aggregateID),
						logging.Error(restoreErr))
				}
			}
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
		// 双缓冲：先解码到临时零值对象做校验，损坏快照/Schema 漂移在此暴露且不污染 target；
		// 校验通过后再解码进 target。注意第二次解码保持 JSON 增量合并语义
		// （仅覆盖载荷中出现的字段），而不是整块替换，以免丢失 target 上
		// 不参与序列化的运行期状态（如内嵌聚合句柄）。
		loadErr := func() error {
			rv := reflect.ValueOf(target)
			if rv.Kind() != reflect.Ptr || rv.IsNil() {
				return jsoncodec.DecodeIntoValue(snapshot.Data, target)
			}
			tmp := reflect.New(rv.Type().Elem())
			if err := jsoncodec.DecodeIntoValue(snapshot.Data, tmp.Interface()); err != nil {
				return err
			}
			return jsoncodec.DecodeIntoValue(snapshot.Data, target)
		}()
		if loadErr != nil {
			if m := sm.getMetrics(); m != nil {
				m.RecordSnapshotLoaded(time.Since(start), false)
			}
			return nil, errors.Wrap(loadErr, errors.InvalidInput, "failed to deserialize snapshot data").
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
