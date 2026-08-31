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

// Create 创建记录。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	return s.createWithPersistence(ctx, entity, func(writeCtx context.Context) error {
		return s.Repository().Create(writeCtx, entity)
	})
}

// CreateAll 批量创建。
func (w *BatchWriter[T, ID]) CreateAll(ctx context.Context, entities []T) error {
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
			by := contextx.Operator(writeCtx)
			if by == "" {
				return errors.NewCode(errors.Internal, "CreateAll: operator not found in tx context (caller bug)")
			}
			now := time.Now()
			records := make([]domaudited.AuditRecord, 0, len(entities))
			for _, entity := range entities {
				snapshot, err := s.marshalAuditSnapshot(entity)
				if err != nil {
					return err
				}
				records = append(records, s.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpCreate, snapshot))
			}
			return s.saveAuditRecords(writeCtx, records)
		},
		PostCommits:             writeflow.PostCommits(callbacksForCreate(entities, s.Application)...),
		CallbackContext:         ctx,
		BeforeValidateOutsideTx: true,
	})
}

func callbacksForCreate[T domain.IEntity[ID], ID comparable](entities []T, app *crud.Application[T, ID]) []func(context.Context) error {
	return writeflow.CallbacksFor(entities, app.PostCommitCreateCallback)
}
