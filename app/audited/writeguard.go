package audited

import (
	"context"

	"gochen/app/internal/writeflow"
	"gochen/domain/access"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// WriteConstraintWriter 以外部包装器形式承载 audited 场景的显式写入约束能力。
type WriteConstraintWriter[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable] struct {
	service *Application[T, ID]
}

// NewWriteConstraintWriter 创建 audited 显式写入约束包装器。
func NewWriteConstraintWriter[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable](service *Application[T, ID]) *WriteConstraintWriter[T, ID] {
	return &WriteConstraintWriter[T, ID]{service: service}
}

func (w *WriteConstraintWriter[T, ID]) serviceOrErr() (*Application[T, ID], error) {
	if w == nil || w.service == nil {
		return nil, errors.NewCode(errors.InvalidInput, "audited write constraint writer service is nil")
	}
	return w.service, nil
}

// CreateWithConstraint 在显式写入约束下创建实体，并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) CreateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited create")
	}

	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.Application.RunBeforeCreate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Application.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return repo.CreateWithConstraint(writeCtx, entity, constraint)
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

// UpdateWithConstraint 在显式写入约束下更新实体，并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) UpdateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited update")
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
			return repo.UpdateWithConstraint(writeCtx, entity, constraint)
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

// DeleteWithConstraint 在显式写入约束下软删实体，并保持审计闭环。
func (w *WriteConstraintWriter[T, ID]) DeleteWithConstraint(ctx context.Context, id ID, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	repo, ok := any(s.Repository()).(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support constrained audited delete")
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
			return repo.UpdateWithConstraint(writeCtx, entity, constraint)
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
