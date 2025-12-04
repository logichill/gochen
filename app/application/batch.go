package application

import (
	"context"
	"fmt"

	"gochen/domain/crud"
	"gochen/errors"
)

// BatchOperationResult 批量操作结果
type BatchOperationResult struct {
	Total      int      `json:"total"`
	Success    int      `json:"success"`
	Failed     int      `json:"failed"`
	SuccessIDs []int64  `json:"success_ids,omitempty"`
	FailedIDs  []int64  `json:"failed_ids,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// CreateBatch 批量创建
func (s *Application[T]) CreateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error) {
	if len(entities) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(entities) > s.config.MaxBatchSize {
		return nil, errors.NewValidationError(fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}

	result := &BatchOperationResult{
		Total:      len(entities),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(crud.IBatchOperations[T, int64]); ok {
		pending := make([]T, 0, len(entities))
		for _, entity := range entities {
			if err := s.BeforeCreate(ctx, entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			if err := s.Validate(entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			pending = append(pending, entity)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.CreateAll(ctx, pending); err != nil {
			return nil, err
		}

		for _, entity := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
			_ = s.AfterCreate(ctx, entity) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, entity := range entities {
		if err := s.Create(ctx, entity); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, entity.GetID())
			result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
		}
	}

	return result, nil
}

// UpdateBatch 批量更新
func (s *Application[T]) UpdateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error) {
	if len(entities) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(entities) > s.config.MaxBatchSize {
		return nil, errors.NewValidationError(fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}

	result := &BatchOperationResult{
		Total:      len(entities),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(crud.IBatchOperations[T, int64]); ok {
		pending := make([]T, 0, len(entities))
		for _, entity := range entities {
			if err := s.BeforeUpdate(ctx, entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			if err := s.Validate(entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			pending = append(pending, entity)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.UpdateBatch(ctx, pending); err != nil {
			return nil, err
		}

		for _, entity := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
			_ = s.AfterUpdate(ctx, entity) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, entity := range entities {
		if err := s.Update(ctx, entity); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, entity.GetID())
			result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
		}
	}

	return result, nil
}

// DeleteBatch 批量删除
func (s *Application[T]) DeleteBatch(ctx context.Context, ids []int64) (*BatchOperationResult, error) {
	if len(ids) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(ids) > s.config.MaxBatchSize {
		return nil, errors.NewValidationError(fmt.Sprintf("batch size exceeds maximum limit of %d", s.config.MaxBatchSize))
	}

	result := &BatchOperationResult{
		Total:      len(ids),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(crud.IBatchOperations[T, int64]); ok {
		pending := make([]int64, 0, len(ids))
		for _, id := range ids {
			if err := s.BeforeDelete(ctx, id); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
				continue
			}

			exists, err := s.Repository().Exists(ctx, id)
			if err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
				continue
			}

			if !exists {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: not found", id))
				continue
			}

			pending = append(pending, id)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.DeleteBatch(ctx, pending); err != nil {
			return nil, err
		}

		for _, id := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, id)
			_ = s.AfterDelete(ctx, id) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, id)
			result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, id)
		}
	}

	return result, nil
}
