package audited

import (
	"context"
	"time"

	"gochen/app/crud"
	"gochen/app/internal/writeflow"
	"gochen/contextx"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// Update 更新记录。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	return s.updateWithPersistence(ctx, entity, func(writeCtx context.Context, entity T) error {
		return s.Repository().Update(writeCtx, entity)
	})
}

// UpdateAll 批量更新。
func (w *BatchWriter[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(entities), s.Config().MaxBatchSize); err != nil {
		return err
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	ids := uniqueEntityIDs(entities)
	expectedVersions, err := versionsByEntityID(entities)
	if err != nil {
		return err
	}
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
			if err := ensureBatchVersionsMatch(beforeByID, expectedVersions, entities); err != nil {
				return err
			}
			for _, entity := range entities {
				if err := s.restoreManagedUpdateFields(beforeByID[entity.GetID()], entity); err != nil {
					return err
				}
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
			by := contextx.Operator(writeCtx)
			if by == "" {
				return errors.NewCode(errors.Internal, "UpdateAll: operator not found in tx context (caller bug)")
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				diff, err := s.computeAuditDiff(beforeByID[entity.GetID()], entity)
				if err != nil {
					return err
				}
				records = append(records, s.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpUpdate, diff))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             writeflow.PostCommits(callbacksForUpdate(entities, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
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

func versionsByEntityID[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable](entities []T) (map[ID]uint64, error) {
	versions := make(map[ID]uint64, len(entities))
	for _, entity := range entities {
		if _, ok := versions[entity.GetID()]; ok {
			return nil, errors.NewCode(errors.InvalidInput, "duplicate entity id in batch update").
				WithContext("id", entity.GetID())
		}
		versions[entity.GetID()] = entity.GetVersion()
	}
	return versions, nil
}

func ensureBatchVersionsMatch[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable](beforeByID map[ID]T, expectedVersions map[ID]uint64, entities []T) error {
	for _, entity := range entities {
		before, ok := beforeByID[entity.GetID()]
		if !ok {
			return errors.NewCode(errors.NotFound, "record not found").
				WithContext("id", entity.GetID())
		}
		want, ok := expectedVersions[entity.GetID()]
		if !ok {
			return errors.NewCode(errors.Internal, "missing expected version").
				WithContext("id", entity.GetID())
		}
		if got := before.GetVersion(); want != got {
			return errors.NewCode(errors.Concurrency, "concurrency conflict").
				WithContext("id", entity.GetID()).
				WithContext("expected_version", want).
				WithContext("actual_version", got)
		}
	}
	return nil
}

func callbacksForUpdate[T domain.IEntity[ID], ID comparable](entities []T, app *crud.Application[T, ID]) []func(context.Context) error {
	return writeflow.CallbacksFor(entities, app.PostCommitUpdateCallback)
}
