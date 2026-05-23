package crud

import (
	"context"

	"gochen/app/internal/writeflow"
	"gochen/domain"
	"gochen/domain/access"
	"gochen/errors"
)

// IWriteConstraintWriter 表示带显式写入约束的写能力。
type IWriteConstraintWriter[T domain.IEntity[ID], ID comparable] interface {
	CreateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error
	UpdateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error
	DeleteWithConstraint(ctx context.Context, id ID, constraint access.WriteConstraint) error
}

// WriteConstraintWriter 以外部包装器形式承载显式写入约束能力，避免污染 Application 主写接口。
type WriteConstraintWriter[T domain.IEntity[ID], ID comparable] struct {
	service *Application[T, ID]
}

// NewWriteConstraintWriter 创建显式写入约束包装器。
func NewWriteConstraintWriter[T domain.IEntity[ID], ID comparable](service *Application[T, ID]) *WriteConstraintWriter[T, ID] {
	return &WriteConstraintWriter[T, ID]{service: service}
}

func (w *WriteConstraintWriter[T, ID]) serviceOrErr() (*Application[T, ID], error) {
	if w == nil || w.service == nil {
		return nil, errors.NewCode(errors.InvalidInput, "write constraint writer service is nil")
	}
	return w.service, nil
}

// CreateWithConstraint 在显式写入约束下创建实体。
func (w *WriteConstraintWriter[T, ID]) CreateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	repo, ok := s.repository.(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support write constraint")
	}
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeCreate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return repo.CreateWithConstraint(writeCtx, entity, constraint)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterCreate(writeCtx, entity)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitCreate(entity)}),
		CallbackContext: ctx,
	})
}

// UpdateWithConstraint 在显式写入约束下更新实体。
func (w *WriteConstraintWriter[T, ID]) UpdateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	repo, ok := s.repository.(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support write constraint")
	}
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeUpdate(writeCtx, entity)
		},
		Validate: func(context.Context) error {
			return s.Validate(entity)
		},
		Write: func(writeCtx context.Context) error {
			return repo.UpdateWithConstraint(writeCtx, entity, constraint)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterUpdate(writeCtx, entity)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitUpdate(entity)}),
		CallbackContext: ctx,
	})
}

// DeleteWithConstraint 在显式写入约束下删除实体。
func (w *WriteConstraintWriter[T, ID]) DeleteWithConstraint(ctx context.Context, id ID, constraint access.WriteConstraint) error {
	s, err := w.serviceOrErr()
	if err != nil {
		return err
	}
	repo, ok := s.repository.(access.IWriteConstraintRepository[T, ID])
	if !ok {
		return errors.NewCode(errors.Unsupported, "repository does not support write constraint")
	}
	return s.runWriteFlow(ctx, writeflow.Plan{
		Before: func(writeCtx context.Context) error {
			return s.runBeforeDelete(writeCtx, id)
		},
		Write: func(writeCtx context.Context) error {
			return repo.DeleteWithConstraint(writeCtx, id, constraint)
		},
		After: func(writeCtx context.Context) error {
			return s.runAfterDelete(writeCtx, id)
		},
		PostCommits:     callbacksToPostCommits([]func(context.Context) error{s.postCommitDelete(id)}),
		CallbackContext: ctx,
	})
}
