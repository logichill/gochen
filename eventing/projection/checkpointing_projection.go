package projection

import (
	"context"

	"gochen/errors"
	"gochen/eventing"
)

// ICheckpointingProjection 表示启用 checkpoint 模式时的强契约：
// 投影必须自行把读模型写入与 checkpoint 保存放进同一原子边界。
//
// HandleWithCheckpoint 的 checkpoint 参数传 nil 表示“只执行读模型写入，
// 不保存位点”（例如批量检查点模式下的中间事件）；实现方必须容忍该哨兵
// 值并保证只提交业务写入，不得在 nil 时报错或仍尝试保存位点。
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

// ICheckpointTxSaver 表示投影能把“仅保存检查点”放入 ORM 事务会话的能力。
//
// 用于多租户跳过路径：非当前租户事件不执行业务处理，但检查点仍需在
// 事务会话内保存。面向接口断言而非具体类型，避免自定义投影或装饰器
// 包装后断言失败，静默降级为无事务保存。
type ICheckpointTxSaver[ID comparable] interface {
	SaveCheckpointWithinTx(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error
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
	// 批量检查点模式下，非保存位点的中间事件同样必须包裹单事件事务：
	// 否则读模型写入脱离事务边界，崩溃/失败后重放会留下半提交状态
	// （P2-046）。CheckpointingProjector 的原子性承诺不能只在保存位点
	// 的那一个事件上成立。
	return p.txRunner.WithinTx(ctx, func(txCtx context.Context) error {
		return p.inner.Handle(txCtx, event)
	})
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

// SaveCheckpointWithinTx 仅在事务会话内保存检查点（不执行业务处理）。
func (p *CheckpointingProjector[ID]) SaveCheckpointWithinTx(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
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
