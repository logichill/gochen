package repo

import (
	"context"
	"strings"
	"time"

	"gochen/contextx"
	"gochen/db/orm"
	"gochen/domain"
	"gochen/domain/audited"
	"gochen/errors"
)

// actorFromContext 优先从上下文读取操作人，缺失时回退到仓储默认 actor。
func (r *Repo[T, ID]) actorFromContext(ctx context.Context) string {
	if v := contextx.Operator(ctx); v != "" {
		return v
	}
	return r.defaultActor
}

// EnsureID 为需要预先派生路径/外键的聚合预分配实体 ID。
func (r *Repo[T, ID]) EnsureID(entity T) error {
	var zeroID ID
	if settable, ok := any(entity).(domain.ISettableID[ID]); ok && entity.GetID() == zeroID {
		if r.idGen == nil {
			return errors.NewCode(errors.InvalidInput, "ID generator not configured (use WithIDGenerator)")
		}
		id, err := r.idGen.Next()
		if err != nil {
			return err
		}
		settable.SetID(id)
	}
	return nil
}

// prepareCreate 在创建前补齐数据域、ID 和审计/时间戳字段。
func (r *Repo[T, ID]) prepareCreate(ctx context.Context, entity T) error {
	if err := r.applyDataScopeToEntity(ctx, entity); err != nil {
		return err
	}
	if err := r.EnsureID(entity); err != nil {
		return err
	}

	if aud, ok := any(entity).(audited.IAuditable); ok {
		by := r.actorFromContext(ctx)
		now := time.Now()
		if aud.GetCreatedAt().IsZero() && strings.TrimSpace(aud.GetCreatedBy()) == "" {
			aud.SetCreatedInfo(by, now)
		}
		aud.SetUpdatedInfo(by, now)
		return nil
	}

	if ts, ok := any(entity).(domain.ITimestamps); ok {
		now := time.Now()
		if ts.GetCreatedAt().IsZero() {
			ts.SetCreatedAt(now)
		}
		ts.SetUpdatedAt(now)
	}

	return nil
}

// prepareUpdate 在更新前补齐数据域和最后修改信息。
func (r *Repo[T, ID]) prepareUpdate(ctx context.Context, entity T) error {
	if err := r.applyDataScopeToEntity(ctx, entity); err != nil {
		return err
	}
	if aud, ok := any(entity).(audited.IAuditable); ok {
		by := r.actorFromContext(ctx)
		aud.SetUpdatedInfo(by, time.Now())
		return nil
	}

	if ts, ok := any(entity).(domain.ITimestamps); ok {
		ts.SetUpdatedAt(time.Now())
	}
	return nil
}

// existsIncludingDeleted 检查记录是否存在，不受软删除过滤影响。
func (r *Repo[T, ID]) existsIncludingDeleted(ctx context.Context, id ID) (bool, error) {
	quer, err := r.query(ctx)
	if err != nil {
		return false, err
	}
	count, err := quer.Where("id = ?", id).Count()
	if err != nil {
		return false, errors.Wrap(err, errors.Database, "failed to check record existence")
	}
	return count > 0, nil
}

// Add 创建并持久化一条实体记录。
func (r *Repo[T, ID]) Add(ctx context.Context, entity T) error {
	if err := r.prepareCreate(ctx, entity); err != nil {
		return err
	}
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	if err := quer.Create(entity); err != nil {
		return errors.Wrap(err, errors.Database, "failed to save record")
	}
	return nil
}

// Update 按主键更新实体，并在启用审计字段时附带乐观锁检查。
func (r *Repo[T, ID]) Update(ctx context.Context, entity T) error {
	expectedVersion := entity.GetVersion()
	if err := r.prepareUpdate(ctx, entity); err != nil {
		return err
	}
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	quer = quer.Where("id = ?", entity.GetID())
	// 乐观锁（仅在启用审计字段时默认打开，因为版本推进语义依赖 SetUpdatedInfo 的约定）。
	if r.auditFields {
		quer = quer.Where(r.versionColumn()+" = ?", expectedVersion)
		if res, err := quer.SaveWithResult(entity); err == nil && res != nil {
			affected, aerr := res.RowsAffected()
			if aerr == nil && affected == 0 {
				exists, err := r.existsIncludingDeleted(ctx, entity.GetID())
				if err != nil {
					return err
				}
				if !exists {
					return errors.NewCode(errors.NotFound, "record not found")
				}
				return errors.NewCode(errors.Concurrency, "concurrent modification detected").
					WithContext("id", entity.GetID()).
					WithContext("expected_version", expectedVersion)
			}
			return nil
		}
		// 不支持 result 的模型：回退为普通更新（不做冲突检测）
	}
	if err := quer.Save(entity); err != nil {
		return errors.Wrap(err, errors.Database, "failed to update record")
	}
	return nil
}

// Delete 删除指定记录；启用软删除时会改写删除标记而非物理删除。
func (r *Repo[T, ID]) Delete(ctx context.Context, id ID) error {
	if r.softDelete {
		now := time.Now()
		by := r.actorFromContext(ctx)
		values := map[string]any{r.softDeleteCols.DeletedAt: now}
		if r.softDeleteCols.DeletedBy != "" {
			values[r.softDeleteCols.DeletedBy] = by
		}
		if r.auditFields {
			values["updated_at"] = now
			values["updated_by"] = by
		}

		quer, err := r.query(ctx)
		if err != nil {
			return err
		}
		quer = quer.Where("id = ?", id).Where(r.softDeleteCols.DeletedAt + " IS NULL")
		if err := quer.UpdateValues(values); err != nil {
			return errors.Wrap(err, errors.Database, "failed to delete record")
		}
		return nil
	}

	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	if err := quer.Where("id = ?", id).Delete(); err != nil {
		return errors.Wrap(err, errors.Database, "failed to delete record")
	}
	return nil
}

// Purge 对指定记录执行物理删除，不保留软删除痕迹。
func (r *Repo[T, ID]) Purge(ctx context.Context, id ID) error {
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	if err := quer.Where("id = ?", id).Delete(); err != nil {
		return errors.Wrap(err, errors.Database, "failed to permanently delete record")
	}
	return nil
}

// inTx 在当前上下文没有活动事务时自行开启一个事务作用域执行回调。
func (r *Repo[T, ID]) inTx(ctx context.Context, fn func(txRepo *Repo[T, ID]) error) error {
	if _, ok := orm.SessionFromContext(ctx); ok {
		return fn(r)
	}
	if session, ok := r.orm.(orm.IOrmSession); ok {
		_ = session
		return fn(r)
	}
	session, err := r.orm.Begin(ctx)
	if err != nil {
		return err
	}
	txRepo, err := r.withOrm(session)
	if err != nil {
		_ = session.Rollback()
		return err
	}
	if err := fn(txRepo); err != nil {
		_ = session.Rollback()
		return err
	}
	if err := session.Commit(); err != nil {
		_ = session.Rollback()
		return err
	}
	return nil
}

// CreateAll 在同一事务里批量创建多条实体记录。
func (r *Repo[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	return r.inTx(ctx, func(txRepo *Repo[T, ID]) error {
		items := make([]any, 0, len(entities))
		for i := range entities {
			if err := txRepo.prepareCreate(ctx, entities[i]); err != nil {
				return err
			}
			items = append(items, entities[i])
		}
		quer, err := txRepo.query(ctx)
		if err != nil {
			return err
		}
		if err := quer.Create(items...); err != nil {
			return errors.Wrap(err, errors.Database, "failed to batch save records")
		}
		return nil
	})
}

// UpdateAll 在同一事务里逐条更新实体。
func (r *Repo[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	return r.inTx(ctx, func(txRepo *Repo[T, ID]) error {
		for i := range entities {
			if err := txRepo.Update(ctx, entities[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteAll 在同一事务里批量删除指定 ID 对应的记录。
func (r *Repo[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if len(ids) == 0 {
		return nil
	}
	return r.inTx(ctx, func(txRepo *Repo[T, ID]) error {
		if txRepo.softDelete {
			now := time.Now()
			by := txRepo.actorFromContext(ctx)
			values := map[string]any{txRepo.softDeleteCols.DeletedAt: now}
			if txRepo.softDeleteCols.DeletedBy != "" {
				values[txRepo.softDeleteCols.DeletedBy] = by
			}
			if txRepo.auditFields {
				values["updated_at"] = now
				values["updated_by"] = by
			}
			quer, err := txRepo.query(ctx)
			if err != nil {
				return err
			}
			quer = quer.Where("id IN ?", ids).Where(txRepo.softDeleteCols.DeletedAt + " IS NULL")
			if err := quer.UpdateValues(values); err != nil {
				return errors.Wrap(err, errors.Database, "failed to batch delete records")
			}
			return nil
		}

		quer, err := txRepo.query(ctx)
		if err != nil {
			return err
		}
		if err := quer.Where("id IN ?", ids).Delete(); err != nil {
			return errors.Wrap(err, errors.Database, "failed to batch delete records")
		}
		return nil
	})
}
