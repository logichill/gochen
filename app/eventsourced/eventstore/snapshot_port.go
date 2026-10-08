package eventstore

import (
	"context"
	"gochen/eventing/store/snapshot"
)

// ISnapshotLoader 提供聚合恢复所需的快照读取能力。
type ISnapshotLoader[ID comparable] interface {
	LoadSnapshot(ctx context.Context, aggregateID ID, target any) (*snapshot.Snapshot[ID], error)
}

// ISnapshotWriter 提供主保存完成后的快照策略与写入能力。
type ISnapshotWriter[ID comparable] interface {
	ShouldCreateSnapshot(ctx context.Context, aggregate snapshot.ISnapshotAggregate[ID]) (bool, error)
	CreateSnapshot(ctx context.Context, aggregateID ID, aggregateType string, data any, version uint64) error
}
