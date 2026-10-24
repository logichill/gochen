package crud

import (
	"context"

	"gochen/app/internal/depcheck"
	"gochen/app/internal/writeflow"
	"gochen/domain/crud"
	"gochen/errors"
)

// CreateAll 批量创建实体，执行校验与生命周期钩子。
func (s *Application[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(entities), s.maxBatchSize()); err != nil {
		return err
	}
	for i, entity := range entities {
		if depcheck.IsNil(entity) {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").WithContext("index", i)
		}
	}
	if err := s.requireBatchCapability("create"); err != nil {
		return err
	}
	return s.flows.createAll.Run(ctx, entities)
}

// HasBatchCapability 报告底层仓储是否支持批量写能力（IBatchOperations 或 ITransactional）。
func (s *Application[T, ID]) HasBatchCapability() bool {
	if s == nil || s.repository == nil {
		return false
	}
	if _, ok := s.repository.(crud.IBatchOperations[T, ID]); ok {
		return true
	}
	return s.txRunner() != nil
}

// requireBatchCapability 在进入写流水线（Before/Validate 钩子）之前先核对仓储
// 是否具备批量写能力。否则静态已知无法执行的写操作仍会先跑完非幂等的 Before
// 准备动作（远程额度预占、外部登记等），且无法回滚。
func (s *Application[T, ID]) requireBatchCapability(action string) error {
	if s.HasBatchCapability() {
		return nil
	}
	return errors.NewCode(errors.Unsupported, "batch "+action+" requires repository to implement IBatchOperations or ITransactional")
}

// UpdateAll 批量更新实体，执行校验与生命周期钩子。
func (s *Application[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(entities), s.maxBatchSize()); err != nil {
		return err
	}
	for i, entity := range entities {
		if depcheck.IsNil(entity) {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").WithContext("index", i)
		}
	}
	if err := s.requireBatchCapability("update"); err != nil {
		return err
	}
	seen := make(map[ID]struct{}, len(entities))
	for _, entity := range entities {
		id := entity.GetID()
		if _, ok := seen[id]; ok {
			return errors.NewCode(errors.InvalidInput, "duplicate entity id in batch update").
				WithContext("id", id)
		}
		seen[id] = struct{}{}
	}
	return s.flows.updateAll.Run(ctx, entities)
}

// DeleteAll 批量删除实体，执行生命周期钩子。
func (s *Application[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if len(ids) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(ids), s.maxBatchSize()); err != nil {
		return err
	}
	if err := s.requireBatchCapability("delete"); err != nil {
		return err
	}
	ids = writeflow.DedupIDs(ids)
	return s.flows.deleteAll.Run(ctx, ids)
}

func (s *Application[T, ID]) maxBatchSize() int {
	cfg := s.serviceConfig()
	return cfg.MaxBatchSize
}
