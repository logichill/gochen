// Package action 提供 L1 安全 PEP：在 Application 编排入口执行动作级权限校验。
//
// 设计要点：
//   - 装饰 crud.IApplication 这一宽接口，并通过 security/internal/capability
//     按内层实际能力组合外壳，保住批量写、审计扩展与装配期注入等
//     **可选能力接口**（api/rest 依赖对它们的类型断言做路由与装配决策）；
//     批量与审计能力面同样经过动作校验，绝不裸转发（见 capability_decorator.go）；
//   - 校验发生在 Application 层而非 Transport 层，
//     故 HTTP / CommandBus / Worker / CLI 全入口语义一致，
//     绕过 REST 直接调用 Application 同样会被拦截；
//   - 本包只认识动作码，不认识租户、数据范围与资源。
//
// 导入约定：本包与 gochen/auth/action 同名，调用方按需起别名，例如
//
//	import secaction "gochen/app/security/action"
package action

import (
	"context"

	authaction "gochen/auth/action"

	"gochen/app/crud"
	"gochen/app/query"
	"gochen/app/security/internal/capability"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/errors"
)

// application 是带动作校验的 crud.IApplication 装饰器。
type application[T domain.IEntity[ID], ID comparable] struct {
	inner   crud.IApplication[T, ID]
	checker authaction.IActionChecker
	policy  authaction.OperationPolicy
}

var _ crud.IApplication[domain.IEntity[int64], int64] = (*application[domain.IEntity[int64], int64])(nil)

// New 以动作级权限校验装饰 Application。
//
// 装配期校验：app / checker 为空，或 policy 中存在非法权限码，直接返回错误，
// 把配置问题暴露在启动期而不是请求期。
func New[T domain.IEntity[ID], ID comparable](
	app crud.IApplication[T, ID],
	checker authaction.IActionChecker,
	policy authaction.OperationPolicy,
) (crud.IApplication[T, ID], error) {
	if app == nil {
		return nil, errors.NewCode(errors.InvalidInput, "application cannot be nil")
	}
	if checker == nil {
		return nil, errors.NewCode(errors.InvalidInput, "action checker cannot be nil")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	core := &application[T, ID]{inner: app, checker: checker, policy: policy}
	return composeCapabilities[T, ID](core, app), nil
}

// composeCapabilities 探测内层可选能力，并为每项能力挂上动作校验后组合外壳。
func composeCapabilities[T domain.IEntity[ID], ID comparable](
	core *application[T, ID],
	inner crud.IApplication[T, ID],
) crud.IApplication[T, ID] {
	detected := capability.Detect[T, ID](inner)
	parts := capability.Parts[T, ID]{Core: core, Aware: detected.Aware}
	if detected.Batch != nil {
		parts.Batch = &batchWriter[T, ID]{app: core, inner: detected.Batch}
	}
	if detected.Audited != nil {
		parts.Audited = &auditedSurface[T, ID]{app: core, inner: detected.Audited}
	}
	return capability.Compose[T, ID](parts)
}

// require 以 fail-closed 方式校验一次操作：策略未配置该操作即拒绝。
func (a *application[T, ID]) require(ctx context.Context, op authaction.Operation) error {
	code, ok := a.policy.CodeFor(op)
	if !ok {
		return errors.NewCode(errors.Forbidden, "operation is not configured in action policy").
			WithContext("operation", string(op))
	}
	return authaction.RequireAction(ctx, a.checker, code)
}

// --- IWriter ---

func (a *application[T, ID]) Create(ctx context.Context, e T) error {
	if err := a.require(ctx, authaction.OpCreate); err != nil {
		return err
	}
	return a.inner.Create(ctx, e)
}

func (a *application[T, ID]) Update(ctx context.Context, e T) error {
	if err := a.require(ctx, authaction.OpUpdate); err != nil {
		return err
	}
	return a.inner.Update(ctx, e)
}

func (a *application[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := a.require(ctx, authaction.OpDelete); err != nil {
		return err
	}
	return a.inner.Delete(ctx, id)
}

// --- IReader ---

func (a *application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := a.require(ctx, authaction.OpRead); err != nil {
		return zero, err
	}
	return a.inner.Get(ctx, id)
}

func (a *application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if err := a.require(ctx, authaction.OpRead); err != nil {
		return false, err
	}
	return a.inner.Exists(ctx, id)
}

func (a *application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.List(ctx, offset, limit)
}

func (a *application[T, ID]) Count(ctx context.Context) (int64, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return 0, err
	}
	return a.inner.Count(ctx)
}

func (a *application[T, ID]) ListByQuery(ctx context.Context, quer *query.QueryRequest) ([]T, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.ListByQuery(ctx, quer)
}

func (a *application[T, ID]) ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.ListPage(ctx, request)
}

func (a *application[T, ID]) CountByQuery(ctx context.Context, quer *query.QueryRequest) (int64, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return 0, err
	}
	return a.inner.CountByQuery(ctx, quer)
}

// --- 非请求路径能力：原样转发，不做动作校验 ---

// Repository 转发底层仓储。
//
// 注意：调用方持有裸 Repository 后可绕过本装饰器直写。这属于 §4.4 定义的
// 进程内可信代码边界，由代码评审而非运行时防御覆盖。
func (a *application[T, ID]) Repository() domcrud.IRepository[T, ID] {
	return a.inner.Repository()
}

func (a *application[T, ID]) QueryRepository() (domcrud.IQueryRepository[T, ID], bool) {
	return a.inner.QueryRepository()
}

func (a *application[T, ID]) Validate(entity T) error {
	return a.inner.Validate(entity)
}

func (a *application[T, ID]) Config() *crud.ServiceConfig {
	return a.inner.Config()
}
