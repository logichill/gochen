package snapshot

import (
	"context"
	"time"

	"gochen/internal/clonevalue"
)

// Snapshot 表示某个聚合在特定版本上的状态快照。
type Snapshot[ID comparable] struct {
	AggregateID   ID             `json:"aggregate_id"`
	AggregateType string         `json:"aggregate_type"`
	Version       uint64         `json:"version"`
	Data          []byte         `json:"data"`
	Timestamp     time.Time      `json:"timestamp"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ISnapshotStore 快照存储接口。
type ISnapshotStore[ID comparable] interface {
	SaveSnapshot(ctx context.Context, snapshot Snapshot[ID]) error
	FindSnapshot(ctx context.Context, aggregateType string, aggregateID ID) (*Snapshot[ID], error)
	DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID ID) error
	ListSnapshots(ctx context.Context, aggregateType string, limit int) ([]Snapshot[ID], error)
	CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error
}

// snapshotKey 生成快照在内存 map 中使用的复合键。
type snapshotKey[ID comparable] struct {
	aggregateType string
	aggregateID   ID
}

func newSnapshotKey[ID comparable](aggregateType string, aggregateID ID) snapshotKey[ID] {
	return snapshotKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
}

func cloneSnapshot[ID comparable](snapshot Snapshot[ID]) Snapshot[ID] {
	if len(snapshot.Data) > 0 {
		snapshot.Data = append([]byte(nil), snapshot.Data...)
	}
	snapshot.Metadata = cloneSnapshotMetadata(snapshot.Metadata)
	return snapshot
}

func cloneSnapshotMetadata(metadata map[string]any) map[string]any {
	return clonevalue.MapStringAny(metadata)
}
