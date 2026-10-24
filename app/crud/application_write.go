package crud

import (
	"context"

	"gochen/app/internal/depcheck"
	"gochen/domain"
	"gochen/errors"
	"gochen/validate"
)

// Create 创建实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	return s.flows.create.Run(ctx, entity)
}

// Update 更新实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	return s.flows.update.Run(ctx, entity)
}

// Delete 删除实体，执行完整的生命周期钩子。
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return s.flows.delete.Run(ctx, id)
}

// Validate 先校验实体领域不变量，再执行可注入的应用策略校验。
func (s *Application[T, ID]) Validate(entity T) error {
	cfg := s.serviceConfig()
	if !cfg.AutoValidate {
		return nil
	}
	if validatable, ok := any(entity).(domain.IValidatable); ok {
		if err := validatable.Validate(); err != nil {
			return err
		}
	}
	if validator := s.validatorSnapshot(); validator != nil {
		return validator.Validate(entity)
	}
	return nil
}

func (s *Application[T, ID]) validatorSnapshot() validate.IValidator {
	return s.validator
}
