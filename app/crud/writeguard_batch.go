package crud

import (
	"context"
	"fmt"

	batchconstraint "gochen/app/internal/writeconstraint"
	"gochen/app/internal/writeflow"
	"gochen/domain"
	"gochen/domain/access"
	"gochen/errors"
)

// IWriteConstraintBatchWriter 表示带显式写入约束的批量写能力。
type IWriteConstraintBatchWriter[T domain.IEntity[ID], ID comparable] interface {
	CreateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error
	UpdateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error
	DeleteAllWithConstraint(ctx context.Context, ids []ID, constraint access.WriteConstraint) error
}

// CreateAllWithConstraint 在显式写入约束下批量创建实体。
func (w *WriteConstraintWriter[T, ID]) CreateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		return nil
	}
	if len(entities) > s.config.MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}
	repo := s.Repository()
	batchRepo, hasBatchRepo := repo.(access.IWriteConstraintBatchRepository[T, ID])
	constraintRepo, hasConstraintRepo := repo.(access.IWriteConstraintRepository[T, ID])
	_, hasTxRepo := s.transactionalRepository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.runBeforeCreate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			if hasBatchRepo {
				return batchRepo.CreateAllWithConstraint(writeCtx, entities, constraint)
			}
			split, err := constraint.SplitByTargets(batchconstraint.TargetsForEntities(ctx, entities))
			if err != nil {
				return err
			}
			if !hasConstraintRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained create requires repository to implement IWriteConstraintBatchRepository or IWriteConstraintRepository")
			}
			if !hasTxRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained create requires repository to implement IWriteConstraintBatchRepository or ITransactional")
			}
			for i, entity := range entities {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				if err := constraintRepo.CreateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After:                   writeflow.ForEach(entities, s.runAfterCreate),
		PostCommits:             callbacksToPostCommits(s.postCommitCreateCallbacks(entities)),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

// UpdateAllWithConstraint 在显式写入约束下批量更新实体。
func (w *WriteConstraintWriter[T, ID]) UpdateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		return nil
	}
	if len(entities) > s.config.MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}
	repo := s.Repository()
	batchRepo, hasBatchRepo := repo.(access.IWriteConstraintBatchRepository[T, ID])
	constraintRepo, hasConstraintRepo := repo.(access.IWriteConstraintRepository[T, ID])
	_, hasTxRepo := s.transactionalRepository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.runBeforeUpdate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			if hasBatchRepo {
				return batchRepo.UpdateAllWithConstraint(writeCtx, entities, constraint)
			}
			split, err := constraint.SplitByTargets(batchconstraint.TargetsForEntities(ctx, entities))
			if err != nil {
				return err
			}
			if !hasConstraintRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained update requires repository to implement IWriteConstraintBatchRepository or IWriteConstraintRepository")
			}
			if !hasTxRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained update requires repository to implement IWriteConstraintBatchRepository or ITransactional")
			}
			for i, entity := range entities {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				if err := constraintRepo.UpdateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After:                   writeflow.ForEach(entities, s.runAfterUpdate),
		PostCommits:             callbacksToPostCommits(s.postCommitUpdateCallbacks(entities)),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

// DeleteAllWithConstraint 在显式写入约束下批量删除实体。
func (w *WriteConstraintWriter[T, ID]) DeleteAllWithConstraint(ctx context.Context, ids []ID, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > s.config.MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}
	repo := s.Repository()
	batchRepo, hasBatchRepo := repo.(access.IWriteConstraintBatchRepository[T, ID])
	constraintRepo, hasConstraintRepo := repo.(access.IWriteConstraintRepository[T, ID])
	_, hasTxRepo := s.transactionalRepository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(ids, s.runBeforeDelete),
		Write: func(writeCtx context.Context) error {
			if hasBatchRepo {
				return batchRepo.DeleteAllWithConstraint(writeCtx, ids, constraint)
			}
			split, err := constraint.SplitByTargets(batchconstraint.TargetsForIDs(ids))
			if err != nil {
				return err
			}
			if !hasConstraintRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained delete requires repository to implement IWriteConstraintBatchRepository or IWriteConstraintRepository")
			}
			if !hasTxRepo {
				return errors.NewCode(errors.Unsupported, "batch constrained delete requires repository to implement IWriteConstraintBatchRepository or ITransactional")
			}
			for i, id := range ids {
				scopedTxCtx, err := split[i].ScopedContext(writeCtx)
				if err != nil {
					return err
				}
				if err := constraintRepo.DeleteWithConstraint(scopedTxCtx, id, split[i]); err != nil {
					return err
				}
			}
			return nil
		},
		After:                   writeflow.ForEach(ids, s.runAfterDelete),
		PostCommits:             callbacksToPostCommits(s.postCommitDeleteCallbacks(ids)),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}
