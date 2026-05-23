package audited

import (
	"context"
	"fmt"
	"time"

	batchconstraint "gochen/app/internal/writeconstraint"
	"gochen/app/internal/writeflow"
	auth "gochen/auth"
	"gochen/domain/access"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// CreateAllWithConstraint 在显式写入约束下批量创建并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) CreateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		return nil
	}
	if len(entities) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited batch create")
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.Application.RunBeforeCreate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Application.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			split, err := constraint.SplitByTargets(batchconstraint.TargetsForEntities(writeCtx, entities))
			if err != nil {
				return err
			}
			for i, entity := range entities {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				if err := repo.CreateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(entities, s.Application.RunAfterCreate)(writeCtx); err != nil {
				return err
			}
			by := authOperatorOrInternalError(writeCtx, "CreateAllWithConstraint")
			if by.err != nil {
				return by.err
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				records = append(records, s.buildAuditRecord(by.value, now, entity.GetID(), domaudited.AuditOpCreate, marshalAuditSnapshot(entity)))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             postCommitCallbacks(callbacksForCreate(entities, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

// UpdateAllWithConstraint 在显式写入约束下批量更新并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) UpdateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		return nil
	}
	if len(entities) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited batch update")
	}

	beforeByID := make(map[ID]T, len(entities))
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.Application.RunBeforeUpdate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Application.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			split, err := constraint.SplitByTargets(batchconstraint.TargetsForEntities(writeCtx, entities))
			if err != nil {
				return err
			}
			for i, entity := range entities {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				before, err := s.Repository().Get(scopedTxCtx, entity.GetID())
				if err != nil {
					return err
				}
				beforeByID[entity.GetID()] = before
				if err := repo.UpdateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(entities, s.Application.RunAfterUpdate)(writeCtx); err != nil {
				return err
			}
			by := authOperatorOrInternalError(writeCtx, "UpdateAllWithConstraint")
			if by.err != nil {
				return by.err
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				records = append(records, s.buildAuditRecord(by.value, now, entity.GetID(), domaudited.AuditOpUpdate, computeEntityDiff(beforeByID[entity.GetID()], entity)))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             postCommitCallbacks(callbacksForUpdate(entities, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

// DeleteAllWithConstraint 在显式写入约束下批量软删并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) DeleteAllWithConstraint(ctx context.Context, ids []ID, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	split, err := constraint.SplitByTargets(batchconstraint.TargetsForIDs(ids))
	if err != nil {
		return err
	}
	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited batch delete")
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(ids, s.Application.RunBeforeDelete),
		Write: func(writeCtx context.Context) error {
			for i, id := range ids {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				entity, err := s.softDeleteEntity(scopedTxCtx, id)
				if err != nil {
					return err
				}
				if err := repo.UpdateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(ids, s.Application.RunAfterDelete)(writeCtx); err != nil {
				return err
			}
			by := authOperatorOrInternalError(writeCtx, "DeleteAllWithConstraint")
			if by.err != nil {
				return by.err
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(ids))
			for _, id := range ids {
				records = append(records, s.buildAuditRecord(by.value, now, id, domaudited.AuditOpDelete, nil))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             postCommitCallbacks(callbacksForDelete(ids, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

type operatorResult struct {
	value string
	err   error
}

func authOperatorOrInternalError(ctx context.Context, op string) operatorResult {
	by := auth.Operator(ctx)
	if by == "" {
		return operatorResult{err: errors.NewCode(errors.Internal, op+": operator not found in tx context (caller bug)")}
	}
	return operatorResult{value: by}
}
