package app

import (
	"context"

	"gochen/auth"
	"gochen/domain"

	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// GetWithDeleted 在授权范围和目标 ID 内读取包含软删状态的实体，冻结写保护基线。
func (r *CommandRepository[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := r.grant.check(ctx); err != nil {
		return zero, err
	}
	if err := r.checkID(id); err != nil {
		return zero, err
	}
	reader, ok := r.repo.(domcrud.IRestoreRepository[T, ID])
	if !ok {
		return zero, errors.NewCode(errors.Unsupported, "command repository does not support deleted reads")
	}
	entity, err := reader.GetWithDeleted(ctx, id, r.grant.decision.Spec)
	if err != nil {
		return zero, err
	}
	return r.track(entity)
}

// Restore 在本次可信读取、授权条件与版本内恢复软删实体；恢复完成后更新保护基线。
func (r *CommandRepository[T, ID]) Restore(ctx context.Context, entity T) error {
	if err := r.grant.check(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "command entity is required")
	}
	if r.grant.action.Kind != auth.KindUpdate {
		return errors.NewCode(errors.Forbidden, "command action cannot restore this resource")
	}
	if err := r.checkWrite(entity); err != nil {
		return err
	}
	restorer, ok := r.repo.(domcrud.IRestoreRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "command repository does not support restore")
	}
	deletable, ok := any(entity).(domain.ISoftDeletable)
	if !ok {
		return errors.NewCode(errors.Unsupported, "command entity does not support restore")
	}
	if !deletable.IsDeleted() {
		return nil
	}
	if err := deletable.Restore(); err != nil {
		return err
	}
	if err := restorer.Restore(ctx, entity, r.grant.decision.Spec); err != nil {
		return err
	}
	_, err := r.track(entity)
	return err
}

// Purge 在本次可信读取、授权条件与版本内永久删除包含软删状态的实体。
func (r *CommandRepository[T, ID]) Purge(ctx context.Context, entity T) error {
	if err := r.grant.check(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "command entity is required")
	}
	if r.grant.action.Kind != auth.KindDelete {
		return errors.NewCode(errors.Forbidden, "command action cannot purge this resource")
	}
	if err := r.checkWrite(entity); err != nil {
		return err
	}
	purger, ok := r.repo.(domcrud.IPurgeRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "command repository does not support purge")
	}
	return purger.Purge(ctx, entity, r.grant.decision.Spec)
}
