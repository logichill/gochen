package audited

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appcrud "gochen/app/crud"
	"gochen/app/internal/writeflow"
	auth "gochen/auth"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

const updateAllBeforeSnapshotChunkSize = 200

// BatchWriter 以外部包装器形式承载 audited 场景的批量写能力。
type BatchWriter[T domain.IEntity[ID], ID comparable] struct {
	service *Application[T, ID]
}

// NewBatchWriter 创建 audited 批量写包装器。
func NewBatchWriter[T domain.IEntity[ID], ID comparable](service *Application[T, ID]) *BatchWriter[T, ID] {
	return &BatchWriter[T, ID]{service: service}
}

func (w *BatchWriter[T, ID]) serviceOrErr() (*Application[T, ID], error) {
	if w == nil || w.service == nil {
		return nil, errors.NewCode(errors.InvalidInput, "audited batch writer service is nil")
	}
	return w.service, nil
}

func (s *Application[T, ID]) runWriteFlow(ctx context.Context, plan writeflow.Plan) error {
	if s.txRepo == nil {
		return errors.NewCode(errors.Internal, "txRepo is nil")
	}
	return writeflow.Run(ctx, s.txRepo, plan)
}

func requireAuditOperator(ctx context.Context) error {
	if auth.Operator(ctx) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	return nil
}

func marshalAuditSnapshot[T any](entity T) json.RawMessage {
	changes, _ := json.Marshal(entity)
	return changes
}

func postCommitCallbacks(callbacks ...func(context.Context) error) []writeflow.PostCommit {
	postCommits := make([]writeflow.PostCommit, 0, len(callbacks))
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		postCommits = append(postCommits, callback)
	}
	return postCommits
}

func (s *Application[T, ID]) buildAuditRecord(by string, now time.Time, id ID, op domaudited.AuditOperation, changes json.RawMessage) domaudited.AuditRecord {
	return domaudited.AuditRecord{
		ID:        0,
		EntityID:  fmt.Sprint(id),
		Operation: op,
		Operator:  by,
		Timestamp: now,
		Changes:   changes,
	}
}

func (s *Application[T, ID]) collectUpdateBeforeSnapshots(ctx context.Context, ids []ID) (map[ID]T, error) {
	beforeByID := make(map[ID]T, len(ids))

	if listRepo, ok := any(s.Repository()).(interface {
		ListByIds(ctx context.Context, ids []ID) ([]T, error)
	}); ok && listRepo != nil {
		for start := 0; start < len(ids); start += updateAllBeforeSnapshotChunkSize {
			end := start + updateAllBeforeSnapshotChunkSize
			if end > len(ids) {
				end = len(ids)
			}
			records, err := listRepo.ListByIds(ctx, ids[start:end])
			if err != nil {
				return nil, err
			}
			for _, before := range records {
				beforeByID[before.GetID()] = before
			}
		}
		for _, id := range ids {
			if _, ok := beforeByID[id]; ok {
				continue
			}
			before, err := s.Repository().Get(ctx, id)
			if err != nil {
				return nil, err
			}
			beforeByID[id] = before
		}
		return beforeByID, nil
	}

	for _, id := range ids {
		before, err := s.Repository().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		beforeByID[id] = before
	}
	return beforeByID, nil
}

func uniqueEntityIDs[T interface{ GetID() ID }, ID comparable](entities []T) []ID {
	ids := make([]ID, 0, len(entities))
	seen := make(map[ID]struct{}, len(entities))
	for _, entity := range entities {
		id := entity.GetID()
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (s *Application[T, ID]) softDeleteEntity(ctx context.Context, id ID) (T, error) {
	var zero T

	by := auth.Operator(ctx)
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

// Create 创建记录。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.Application.RunBeforeCreate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Application.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return s.Repository().Create(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterCreate(writeCtx, entity); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, entity.GetID(), domaudited.AuditOpCreate, marshalAuditSnapshot(entity))
		},
		PostCommits:     postCommitCallbacks(s.Application.PostCommitCreateCallback(entity)),
		CallbackContext: ctx,
	})
}

// Update 更新记录。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	var before T
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.Application.RunBeforeUpdate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Application.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			var err error
			before, err = s.Repository().Get(writeCtx, entity.GetID())
			if err != nil {
				return err
			}
			return s.Repository().Update(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterUpdate(writeCtx, entity); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, entity.GetID(), domaudited.AuditOpUpdate, computeEntityDiff(before, entity))
		},
		PostCommits:     postCommitCallbacks(s.Application.PostCommitUpdateCallback(entity)),
		CallbackContext: ctx,
	})
}

// Delete 执行软删除（透传到 ISoftDeletable 实现）：先 Before 钩子、再 softDeleteEntity+Update、再 After 钩子并写入审计 AuditOpDelete。
//
// 约束：
// - ctx 中必须包含 operator，否则返回 InvalidInput；
// - 物理删除请使用 Purge。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.Application.RunBeforeDelete(writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			entity, err := s.softDeleteEntity(writeCtx, id)
			if err != nil {
				return err
			}
			return s.Repository().Update(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterDelete(writeCtx, id); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, id, domaudited.AuditOpDelete, nil)
		},
		PostCommits:     postCommitCallbacks(s.Application.PostCommitDeleteCallback(id)),
		CallbackContext: ctx,
	})
}

// Purge 执行物理删除（永久删除），并记录审计。
//
// 约束：
// - ctx 中必须包含 operator；
// - repo 必须实现 crud.IPurgeRepository，否则返回 InvalidInput。
func (s *Application[T, ID]) Purge(ctx context.Context, id ID) error {
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	r, ok := any(s.Repository()).(crud.IPurgeRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "repository does not support purge")
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.Application.RunBeforeDelete(writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			return r.Purge(writeCtx, id)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterDelete(writeCtx, id); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, id, domaudited.AuditOpDeleteHard, nil)
		},
	})
}

// Restore 恢复已软删的实体，并记录审计。
//
// 参数：
// - by：本次恢复操作的 operator（会写入审计记录）。
func (s *Application[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}

	var auditCtx context.Context
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
			if err := ae.Restore(); err != nil {
				return err
			}
			if err := s.Repository().Update(writeCtx, entity); err != nil {
				return err
			}
			auditCtx, err = auth.WithOperator(writeCtx, by)
			return err
		},
		After: func(context.Context) error {
			return s.saveAudit(auditCtx, id, domaudited.AuditOpRestore, nil)
		},
	})
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
	if len(entities) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo := s.Repository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.Application.RunBeforeCreate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Application.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			if batchRepo, ok := repo.(interface {
				CreateAll(ctx context.Context, entities []T) error
			}); ok {
				return batchRepo.CreateAll(writeCtx, entities)
			}
			for _, entity := range entities {
				if err := repo.Create(writeCtx, entity); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(entities, s.Application.RunAfterCreate)(writeCtx); err != nil {
				return err
			}
			by := auth.Operator(writeCtx)
			if by == "" {
				return errors.NewCode(errors.Internal, "CreateAll: operator not found in tx context (caller bug)")
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				records = append(records, s.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpCreate, marshalAuditSnapshot(entity)))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             postCommitCallbacks(callbacksForCreate(entities, s.Application)...),
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
	if len(entities) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	ids := uniqueEntityIDs(entities)
	beforeByID := make(map[ID]T, len(ids))
	repo := s.Repository()
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: writeflow.ForEach(entities, s.Application.RunBeforeUpdate),
		Validate: writeflow.ForEach(entities, func(_ context.Context, entity T) error {
			return s.Application.Validate(entity)
		}),
		Write: func(writeCtx context.Context) error {
			var err error
			beforeByID, err = s.collectUpdateBeforeSnapshots(writeCtx, ids)
			if err != nil {
				return err
			}
			if batchRepo, ok := repo.(interface {
				UpdateAll(ctx context.Context, entities []T) error
			}); ok {
				return batchRepo.UpdateAll(writeCtx, entities)
			}
			for _, entity := range entities {
				if err := repo.Update(writeCtx, entity); err != nil {
					return err
				}
			}
			return nil
		},
		After: func(writeCtx context.Context) error {
			if err := writeflow.ForEach(entities, s.Application.RunAfterUpdate)(writeCtx); err != nil {
				return err
			}
			by := auth.Operator(writeCtx)
			if by == "" {
				return errors.NewCode(errors.Internal, "UpdateAll: operator not found in tx context (caller bug)")
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				records = append(records, s.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpUpdate, computeEntityDiff(beforeByID[entity.GetID()], entity)))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             postCommitCallbacks(callbacksForUpdate(entities, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
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
	if len(ids) > s.Config().MaxBatchSize {
		return errors.NewCode(errors.Validation, fmt.Sprintf("batch size exceeds maximum limit of %d", s.Config().MaxBatchSize))
	}
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
			by := auth.Operator(writeCtx)
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
		PostCommits:             postCommitCallbacks(callbacksForDelete(ids, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

func callbacksForCreate[T domain.IEntity[ID], ID comparable](entities []T, app *appcrud.Application[T, ID]) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(entities))
	for _, entity := range entities {
		if cb := app.PostCommitCreateCallback(entity); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}

func callbacksForUpdate[T domain.IEntity[ID], ID comparable](entities []T, app *appcrud.Application[T, ID]) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(entities))
	for _, entity := range entities {
		if cb := app.PostCommitUpdateCallback(entity); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}

func callbacksForDelete[T domain.IEntity[ID], ID comparable](ids []ID, app *appcrud.Application[T, ID]) []func(context.Context) error {
	callbacks := make([]func(context.Context) error, 0, len(ids))
	for _, id := range ids {
		if cb := app.PostCommitDeleteCallback(id); cb != nil {
			callbacks = append(callbacks, cb)
		}
	}
	return callbacks
}
