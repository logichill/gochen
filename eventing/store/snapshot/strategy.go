package snapshot

import (
	"context"
)

// ISnapshotAggregate 快照聚合接口。
// 避免循环依赖，定义快照所需的最小接口。
type ISnapshotAggregate[ID comparable] interface {
	GetID() ID
	GetVersion() uint64
	GetAggregateType() string
}

// ISnapshotStrategy 快照策略接口。
// 用于判断是否应该为聚合根创建快照。
type ISnapshotStrategy[ID comparable] interface {
	ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error)
	Name() string // 策略名称
}
