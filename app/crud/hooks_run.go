package crud

import (
	"context"
	"gochen/app/internal/writeflow"

	"gochen/domain"
)

// RunBeforeDelete 执行显式 Hooks.BeforeDelete；未配置则 no-op。
func (s *Application[T, ID]) RunBeforeDelete(ctx context.Context, id ID) error {
	return s.runBeforeDelete(ctx, id)
}

// RunAfterDelete 执行显式 Hooks.AfterDelete；未配置则 no-op。
func (s *Application[T, ID]) RunAfterDelete(ctx context.Context, id ID) error {
	return s.runAfterDelete(ctx, id)
}

func runBeforeCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.BeforeCreate == nil {
		return nil
	}
	return hooks.BeforeCreate(ctx, entity)
}

func runAfterCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.AfterCreate == nil {
		return nil
	}
	return hooks.AfterCreate(ctx, entity)
}

func postCommitCreateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], entity T) func(context.Context) error {
	if hooks == nil || hooks.PostCommitCreate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitCreate(ctx, entity)
	}
}

func runBeforeUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.BeforeUpdate == nil {
		return nil
	}
	return hooks.BeforeUpdate(ctx, entity)
}

func runAfterUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, entity T) error {
	if hooks == nil || hooks.AfterUpdate == nil {
		return nil
	}
	return hooks.AfterUpdate(ctx, entity)
}

func postCommitUpdateHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], entity T) func(context.Context) error {
	if hooks == nil || hooks.PostCommitUpdate == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitUpdate(ctx, entity)
	}
}

func runBeforeDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, id ID) error {
	if hooks == nil || hooks.BeforeDelete == nil {
		return nil
	}
	return hooks.BeforeDelete(ctx, id)
}

func runAfterDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], ctx context.Context, id ID) error {
	if hooks == nil || hooks.AfterDelete == nil {
		return nil
	}
	return hooks.AfterDelete(ctx, id)
}

func postCommitDeleteHook[T domain.IEntity[ID], ID comparable](hooks *Hooks[T, ID], id ID) func(context.Context) error {
	if hooks == nil || hooks.PostCommitDelete == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return hooks.PostCommitDelete(ctx, id)
	}
}

// runBeforeCreate 执行显式配置的 BeforeCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeCreate(ctx context.Context, entity T) error {
	return runBeforeCreateHook(s.hooksSnapshot(), ctx, entity)
}

// runAfterCreate 执行显式配置的 AfterCreate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterCreate(ctx context.Context, entity T) error {
	return runAfterCreateHook(s.hooksSnapshot(), ctx, entity)
}

func (s *Application[T, ID]) postCommitCreate(entity T) func(context.Context) error {
	return postCommitCreateHook(s.hooksSnapshot(), entity)
}

// runBeforeUpdate 执行显式配置的 BeforeUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeUpdate(ctx context.Context, entity T) error {
	return runBeforeUpdateHook(s.hooksSnapshot(), ctx, entity)
}

// runAfterUpdate 执行显式配置的 AfterUpdate hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterUpdate(ctx context.Context, entity T) error {
	return runAfterUpdateHook(s.hooksSnapshot(), ctx, entity)
}

func (s *Application[T, ID]) postCommitUpdate(entity T) func(context.Context) error {
	return postCommitUpdateHook(s.hooksSnapshot(), entity)
}

// runBeforeDelete 执行显式配置的 BeforeDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runBeforeDelete(ctx context.Context, id ID) error {
	return runBeforeDeleteHook(s.hooksSnapshot(), ctx, id)
}

// runAfterDelete 执行显式配置的 AfterDelete hook；未配置则 no-op。
func (s *Application[T, ID]) runAfterDelete(ctx context.Context, id ID) error {
	return runAfterDeleteHook(s.hooksSnapshot(), ctx, id)
}

func (s *Application[T, ID]) postCommitDelete(id ID) func(context.Context) error {
	return postCommitDeleteHook(s.hooksSnapshot(), id)
}

func (s *Application[T, ID]) postCommitCreateCallbacks(entities []T) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(entities, func(entity T) func(context.Context) error {
		return postCommitCreateHook(hooks, entity)
	})
}

func (s *Application[T, ID]) postCommitUpdateCallbacks(entities []T) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(entities, func(entity T) func(context.Context) error {
		return postCommitUpdateHook(hooks, entity)
	})
}

func (s *Application[T, ID]) postCommitDeleteCallbacks(ids []ID) []func(context.Context) error {
	hooks := s.hooksSnapshot()
	return writeflow.CallbacksFor(ids, func(id ID) func(context.Context) error {
		return postCommitDeleteHook(hooks, id)
	})
}
