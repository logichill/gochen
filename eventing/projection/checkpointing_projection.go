package projection

import (
	"context"

	"gochen/errors"
	"gochen/eventing"
)

// ICheckpointingProjection 表示启用 checkpoint 模式时的强契约：
// 投影必须自行把读模型写入与 checkpoint 保存放进同一原子边界。
type ICheckpointingProjection[ID comparable] interface {
	IProjection[ID]
	HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error
}

// IRebuildCheckpointingProjection 表示投影支持在重建阶段把读模型重放与最终 checkpoint 保存放进同一原子边界。
type IRebuildCheckpointingProjection[ID comparable] interface {
	ICheckpointingProjection[ID]
	RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[ID], store ICheckpointStore, checkpoint *Checkpoint) error
}

// IProjectionTxRunner 表示投影侧可复用的最小事务能力。
type IProjectionTxRunner interface {
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// CheckpointingProjector 为普通 projection 提供默认的“事务内处理 + checkpoint 保存”封装。
type CheckpointingProjector[ID comparable] struct {
	inner    IProjection[ID]
	txRunner IProjectionTxRunner
}

func NewCheckpointingProjector[ID comparable](inner IProjection[ID], txRunner IProjectionTxRunner) (*CheckpointingProjector[ID], error) {
	if inner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "projection cannot be nil")
	}
	if txRunner == nil {
		return nil, errors.NewCode(errors.InvalidInput, "projection tx runner cannot be nil")
	}
	return &CheckpointingProjector[ID]{inner: inner, txRunner: txRunner}, nil
}

func (p *CheckpointingProjector[ID]) Name() string { return p.inner.Name() }

func (p *CheckpointingProjector[ID]) Handle(ctx context.Context, event eventing.IEvent) error {
	return p.inner.Handle(ctx, event)
}

func (p *CheckpointingProjector[ID]) SupportedEventTypes() []string {
	return p.inner.SupportedEventTypes()
}

func (p *CheckpointingProjector[ID]) Rebuild(ctx context.Context, events []eventing.Event[ID]) error {
	return p.inner.Rebuild(ctx, events)
}

func (p *CheckpointingProjector[ID]) Status() ProjectionStatus { return p.inner.Status() }

func (p *CheckpointingProjector[ID]) HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error {
	return p.txRunner.WithinTx(ctx, func(txCtx context.Context) error {
		if err := p.inner.Handle(txCtx, event); err != nil {
			return err
		}
		return saveCheckpoint(txCtx, store, checkpoint)
	})
}

func (p *CheckpointingProjector[ID]) saveCheckpointWithinTx(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	return p.txRunner.WithinTx(ctx, func(txCtx context.Context) error {
		return saveCheckpoint(txCtx, store, checkpoint)
	})
}

func (p *CheckpointingProjector[ID]) RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[ID], store ICheckpointStore, checkpoint *Checkpoint) error {
	return p.txRunner.WithinTx(ctx, func(txCtx context.Context) error {
		if err := p.inner.Rebuild(txCtx, events); err != nil {
			return err
		}
		return saveRebuildCheckpoint(txCtx, store, checkpoint)
	})
}

func saveCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if store == nil || checkpoint == nil {
		return nil
	}
	return store.Save(ctx, checkpoint)
}

// saveRebuildCheckpoint 在重建结束时把游标无条件对齐到重建结果。
//
// 重建会把读模型重放到 events 的最终位置，该位置可能低于已持久化游标；若仍走只前进的
// Save，会出现“读模型已回退、持久游标仍停在旧位置”，恢复时跳过区间事件。故优先使用
// 支持 ForceSave 的 store；不支持时退回“先删后存”，使后续 Save 不再受旧位置限制。
func saveRebuildCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if store == nil || checkpoint == nil {
		return nil
	}
	return forceSaveCheckpoint(ctx, store, checkpoint)
}

func forceSaveCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if forcer, ok := store.(ICheckpointForceSaver); ok {
		return forcer.ForceSave(ctx, checkpoint)
	}
	if err := store.Delete(ctx, checkpoint.ProjectionName); err != nil {
		return err
	}
	return store.Save(ctx, checkpoint)
}

type rebuildCheckpointStore struct {
	inner ICheckpointStore
}

func newRebuildCheckpointStore(store ICheckpointStore) ICheckpointStore {
	if store == nil {
		return nil
	}
	return rebuildCheckpointStore{inner: store}
}

func (s rebuildCheckpointStore) Load(ctx context.Context, projectionName string) (*Checkpoint, error) {
	return s.inner.Load(ctx, projectionName)
}

func (s rebuildCheckpointStore) Save(ctx context.Context, checkpoint *Checkpoint) error {
	return forceSaveCheckpoint(ctx, s.inner, checkpoint)
}

func (s rebuildCheckpointStore) Delete(ctx context.Context, projectionName string) error {
	return s.inner.Delete(ctx, projectionName)
}

func (s rebuildCheckpointStore) ForceSave(ctx context.Context, checkpoint *Checkpoint) error {
	return forceSaveCheckpoint(ctx, s.inner, checkpoint)
}

func (s rebuildCheckpointStore) RequiresORMTxSession() bool {
	requirer, ok := s.inner.(ICheckpointTxSessionRequirer)
	return ok && requirer.RequiresORMTxSession()
}

func validateCheckpointingProjection[ID comparable](projection IProjection[ID], store ICheckpointStore) error {
	if projection == nil || store == nil {
		return nil
	}
	if tenantProjection, ok := projection.(*TenantAwareProjector[ID]); ok {
		return validateCheckpointingProjection(tenantProjection.projector, store)
	}
	if _, ok := projection.(ICheckpointingProjection[ID]); ok {
		if _, ok := projection.(IRebuildCheckpointingProjection[ID]); ok {
			return nil
		}
		return errors.NewCode(errors.Unsupported, "projection does not support checkpoint rebuild mode").
			WithContext("projection", projection.Name())
	}
	return errors.NewCode(errors.Unsupported, "projection does not support checkpoint mode").
		WithContext("projection", projection.Name())
}
