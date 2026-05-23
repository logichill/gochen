package projection

import (
	"context"
	"database/sql"

	"gochen/db/orm"
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

// SQLCheckpointTxRunner opens an ORM transaction and exposes the session through ctx.
type SQLCheckpointTxRunner struct {
	ormEngine orm.IOrm
}

func NewSQLCheckpointTxRunner(ormEngine orm.IOrm) (*SQLCheckpointTxRunner, error) {
	if ormEngine == nil {
		return nil, errors.NewCode(errors.InvalidInput, "projection orm cannot be nil")
	}
	return &SQLCheckpointTxRunner{ormEngine: ormEngine}, nil
}

func (r *SQLCheckpointTxRunner) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if r == nil || r.ormEngine == nil {
		return errors.NewCode(errors.InvalidInput, "projection orm cannot be nil")
	}
	if fn == nil {
		return errors.NewCode(errors.InvalidInput, "fn is nil")
	}

	session, err := r.ormEngine.BeginTx(ctx, (*sql.TxOptions)(nil))
	if err != nil {
		return errors.Wrap(err, errors.Database, "begin projection transaction failed")
	}
	txCtx, err := orm.WithTxSession(ctx, session, true)
	if err != nil {
		_ = session.Rollback()
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback()
		}
	}()

	if err := fn(txCtx); err != nil {
		return err
	}
	if err := session.Commit(); err != nil {
		return errors.Wrap(err, errors.Database, "commit projection transaction failed")
	}
	committed = true
	return nil
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

func (p *CheckpointingProjector[ID]) RebuildWithCheckpoint(ctx context.Context, events []eventing.Event[ID], store ICheckpointStore, checkpoint *Checkpoint) error {
	return p.txRunner.WithinTx(ctx, func(txCtx context.Context) error {
		if err := p.inner.Rebuild(txCtx, events); err != nil {
			return err
		}
		return saveCheckpoint(txCtx, store, checkpoint)
	})
}

func saveCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if store == nil || checkpoint == nil {
		return nil
	}
	if err := validateCheckpointTransaction(ctx, store); err != nil {
		return err
	}
	return store.Save(ctx, checkpoint)
}

func validateCheckpointTransaction(ctx context.Context, store ICheckpointStore) error {
	requirer, ok := store.(ICheckpointTxSessionRequirer)
	if !ok || !requirer.RequiresORMTxSession() {
		return nil
	}
	session, ok := orm.SessionFromContext(ctx)
	if !ok || session == nil {
		return errors.NewCode(errors.Unsupported, "sql checkpoint store requires an orm transaction session in context")
	}
	if session.Database() == nil {
		return errors.NewCode(errors.Unsupported, "sql checkpoint transaction session has no database handle")
	}
	return nil
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
