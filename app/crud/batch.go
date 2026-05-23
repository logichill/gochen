package crud

import (
	"context"
	"fmt"

	"gochen/app/internal/writeflow"
	"gochen/domain"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

// BatchWriter 以外部包装器形式承载批量写能力，避免污染 Application 主接口。
type BatchWriter[T domain.IEntity[ID], ID comparable] struct {
	service *Application[T, ID]
}

// NewBatchWriter 创建批量写包装器。
func NewBatchWriter[T domain.IEntity[ID], ID comparable](service *Application[T, ID]) *BatchWriter[T, ID] {
	return &BatchWriter[T, ID]{service: service}
}

func (w *BatchWriter[T, ID]) serviceOrErr() (*Application[T, ID], error) {
	if w == nil || w.service == nil {
		return nil, errors.NewCode(errors.InvalidInput, "batch writer service is nil")
	}
	return w.service, nil
}

// CreateAll 创建全部。
func (w *BatchWriter[T, ID]) CreateAll(ctx context.Context, entities []T) error {
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
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.runBeforeCreate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			if batchRepo, ok := repo.(domaincrud.IBatchOperations[T, ID]); ok {
				return batchRepo.CreateAll(writeCtx, entities)
			}
			if _, ok := s.transactionalRepository(); !ok {
				return errors.NewCode(errors.Unsupported, "batch create requires repository to implement IBatchOperations or ITransactional")
			}
			for _, entity := range entities {
				if err := repo.Create(writeCtx, entity); err != nil {
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

// UpdateAll 更新全部。
func (w *BatchWriter[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
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
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.runBeforeUpdate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			if batchRepo, ok := repo.(domaincrud.IBatchOperations[T, ID]); ok {
				return batchRepo.UpdateAll(writeCtx, entities)
			}
			if _, ok := s.transactionalRepository(); !ok {
				return errors.NewCode(errors.Unsupported, "batch update requires repository to implement IBatchOperations or ITransactional")
			}
			for _, entity := range entities {
				if err := repo.Update(writeCtx, entity); err != nil {
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

// DeleteAll 删除全部。
func (w *BatchWriter[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
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
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(ids, s.runBeforeDelete),
		Write: func(writeCtx context.Context) error {
			if batchRepo, ok := repo.(domaincrud.IBatchOperations[T, ID]); ok {
				return batchRepo.DeleteAll(writeCtx, ids)
			}
			if _, ok := s.transactionalRepository(); !ok {
				return errors.NewCode(errors.Unsupported, "batch delete requires repository to implement IBatchOperations or ITransactional")
			}
			for _, id := range ids {
				if err := repo.Delete(writeCtx, id); err != nil {
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
