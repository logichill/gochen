package audited

import (
	"context"
	"strings"
	"time"

	appcrud "gochen/app/crud"
	"gochen/app/internal/writeflow"
	"gochen/contextx"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

// Delete 执行软删除（透传到 ISoftDeletable 实现）：先 Before 钩子、再 softDeleteEntity+Update、再 After 钩子并写入审计 AuditOpDelete。
//
// 约束：
// - ctx 中必须包含 operator，否则返回 InvalidInput；
// - 物理删除请使用 Purge。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return s.deleteWithPersistence(ctx, id, func(writeCtx context.Context, entity T) error {
		return s.Repository().Update(writeCtx, entity)
	})
}

// Purge 执行物理删除（永久删除），并记录审计。
//
// 约束：
// - ctx 中必须包含 operator；
// - repo 必须实现 crud.IPurgeRepository，否则返回 InvalidInput。
func (s *Application[T, ID]) Purge(ctx context.Context, id ID) error {
	r, ok := any(s.Repository()).(crud.IPurgeRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "repository does not support purge")
	}

	return s.purgeCore(ctx, id, nil, func(writeCtx context.Context) error {
		return r.Purge(writeCtx, id)
	})
}

func (s *Application[T, ID]) purgeCore(ctx context.Context, id ID, beforeFn func(context.Context) error, writeFn func(context.Context) error) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			if beforeFn != nil {
				if err := beforeFn(writeCtx); err != nil {
					return err
				}
			}
			return s.Application.RunBeforeDelete(writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			return writeFn(writeCtx)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterDelete(writeCtx, id); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, id, domaudited.AuditOpDeleteHard, nil)
		},
		PostCommits:     writeflow.PostCommits(s.Application.PostCommitDeleteCallback(id)),
		CallbackContext: ctx,
	})
}

// Restore 恢复已软删的实体，并记录审计。
//
// 参数：
// - by：本次恢复操作的 operator（会写入审计记录）。
func (s *Application[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	return s.restoreCore(ctx, id, by, func(writeCtx context.Context, entity T) error {
		return s.Repository().Update(writeCtx, entity)
	})
}

func (s *Application[T, ID]) restoreCore(ctx context.Context, id ID, by string, writeFn func(context.Context, T) error) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}

	var auditCtx context.Context
	var restored T
	return s.runWriteFlow(ctx, writeflow.Plan{
		Write: func(writeCtx context.Context) error {
			entity, err := s.restoreRepo.GetWithDeleted(writeCtx, id)
			if err != nil {
				return err
			}
			ae, err := s.asAuditedEntity(entity)
			if err != nil {
				return err
			}
			if !ae.IsDeleted() {
				return errors.NewCode(errors.Conflict, "entity not deleted").WithContext("id", id)
			}
			if err := ae.Restore(); err != nil {
				return err
			}
			if err := writeFn(writeCtx, entity); err != nil {
				return err
			}
			restored = entity
			auditCtx, err = contextx.WithOperator(writeCtx, by)
			return err
		},
		After: func(context.Context) error {
			return s.saveAudit(auditCtx, id, domaudited.AuditOpRestore, nil)
		},
		PostCommits: writeflow.PostCommits(func(cbCtx context.Context) error {
			cb := s.Application.PostCommitUpdateCallback(restored)
			if cb == nil {
				return nil
			}
			return cb(cbCtx)
		}),
	})
}

func (s *Application[T, ID]) softDeleteEntity(ctx context.Context, id ID) (T, error) {
	var zero T

	by := contextx.Operator(ctx)
	if by == "" {
		return zero, errors.NewCode(errors.InvalidInput, "audit operator is required")
	}

	entity, err := s.Repository().Get(ctx, id)
	if err != nil {
		return zero, err
	}
	ae, err := s.asAuditedEntity(entity)
	if err != nil {
		return zero, err
	}
	if err := ae.SoftDeleteBy(by, time.Now()); err != nil {
		return zero, err
	}
	return entity, nil
}

// DeleteAll 批量软删除，为每个实体写入 DELETE 审计记录。
func (w *BatchWriter[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(ids), s.Config().MaxBatchSize); err != nil {
		return err
	}
	ids = dedupIDs(ids)
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo := s.Repository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(ids, s.Application.RunBeforeDelete),
		Write: func(writeCtx context.Context) error {
			for _, id := range ids {
				entity, err := s.softDeleteEntity(writeCtx, id)
				if err != nil {
					return err
				}
				if err := repo.Update(writeCtx, entity); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(ids, s.Application.RunAfterDelete)(writeCtx); err != nil {
				return err
			}
			by := contextx.Operator(writeCtx)
			if by == "" {
				return errors.NewCode(errors.Internal, "DeleteAll: operator not found in tx context (caller bug)")
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(ids))
			for _, id := range ids {
				records = append(records, s.buildAuditRecord(by, now, id, domaudited.AuditOpDelete, nil))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             writeflow.PostCommits(callbacksForDelete(ids, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

// dedupIDs 保持首次出现顺序去重，使批量软删对重复 ID 幂等（与 repo 层 IN 语义对齐）。
func dedupIDs[ID comparable](ids []ID) []ID {
	if len(ids) <= 1 {
		return ids
	}
	seen := make(map[ID]struct{}, len(ids))
	out := make([]ID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func callbacksForDelete[T domain.IEntity[ID], ID comparable](ids []ID, app *appcrud.Application[T, ID]) []func(context.Context) error {
	return writeflow.CallbacksFor(ids, app.PostCommitDeleteCallback)
}
