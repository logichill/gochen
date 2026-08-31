package audited

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gochen/app/internal/writeflow"
	"gochen/contextx"
	"gochen/domain"
	domaudited "gochen/domain/audited"
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
	if contextx.Operator(ctx) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	return nil
}

func (s *Application[T, ID]) createWithPersistence(
	ctx context.Context,
	entity T,
	persist func(context.Context) error,
) error {
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
			return persist(writeCtx)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterCreate(writeCtx, entity); err != nil {
				return err
			}
			snapshot, err := s.marshalAuditSnapshot(entity)
			if err != nil {
				return err
			}
			return s.saveAudit(writeCtx, entity.GetID(), domaudited.AuditOpCreate, snapshot)
		},
		PostCommits:     writeflow.PostCommits(s.Application.PostCommitCreateCallback(entity)),
		CallbackContext: ctx,
	})
}

func (s *Application[T, ID]) updateWithPersistence(
	ctx context.Context,
	entity T,
	persist func(context.Context, T) error,
) error {
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
			if err := s.restoreManagedUpdateFields(before, entity); err != nil {
				return err
			}
			return persist(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterUpdate(writeCtx, entity); err != nil {
				return err
			}
			diff, err := s.computeAuditDiff(before, entity)
			if err != nil {
				return err
			}
			return s.saveAudit(writeCtx, entity.GetID(), domaudited.AuditOpUpdate, diff)
		},
		PostCommits:     writeflow.PostCommits(s.Application.PostCommitUpdateCallback(entity)),
		CallbackContext: ctx,
	})
}

func (s *Application[T, ID]) deleteWithPersistence(
	ctx context.Context,
	id ID,
	persist func(context.Context, T) error,
) error {
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
			return persist(writeCtx, entity)
		},
		After: func(writeCtx context.Context) error {
			if err := s.Application.RunAfterDelete(writeCtx, id); err != nil {
				return err
			}
			return s.saveAudit(writeCtx, id, domaudited.AuditOpDelete, nil)
		},
		PostCommits:     writeflow.PostCommits(s.Application.PostCommitDeleteCallback(id)),
		CallbackContext: ctx,
	})
}

func (s *Application[T, ID]) buildAuditRecord(by string, now time.Time, id ID, op domaudited.AuditOperation, changes json.RawMessage) domaudited.AuditRecord {
	return domaudited.AuditRecord{
		ID:           0,
		ResourceKind: s.resourceKind,
		EntityID:     fmt.Sprint(id),
		Operation:    op,
		Operator:     by,
		Timestamp:    now,
		Changes:      changes,
	}
}

func uniqueEntityIDs[T interface{ GetID() ID }, ID comparable](entities []T) []ID {
	ids := make([]ID, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, entity.GetID())
	}
	return dedupIDs(ids)
}
