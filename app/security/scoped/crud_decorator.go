// Package scoped 提供 L3 安全 PEP：在 Application 编排入口完成资源授权，
// 并把写约束经受控通道投放给仓储。
//
// 设计要点：
//   - 装饰 crud.IApplication 这一宽接口，并通过 security/internal/capability
//     按内层实际能力组合外壳，保住批量写与审计扩展等可选能力接口；
//     批量与审计高危操作各自做资源授权（见 batch_decorator.go、audited_decorator.go）；
//   - 基础 Application（crud / audited / eventsourced）对约束零感知、零依赖：
//     约束不进入任何公共接口，只经 auth/scoped 的受控 ctx 通道传递；
//   - 构造期断言仓储已声明范围列，杜绝"约束被静默丢弃、请求全部放行"。
//
// 导入约定：本包与 gochen/auth/scoped 同名，调用方按需起别名，例如
//
//	import secscoped "gochen/app/security/scoped"
package scoped

import (
	"gochen/app/crud"
	"gochen/app/security/internal/capability"
	authscoped "gochen/auth/scoped"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/errors"
)

// application 是带资源授权与约束投放的 crud.IApplication 装饰器。
type application[T domain.IEntity[ID], ID comparable] struct {
	inner        crud.IApplication[T, ID]
	repository   domcrud.IRepository[T, ID]
	maxBatchSize int
	authorizer   authscoped.IAuthorizer
	config       Config
}

var _ crud.IApplication[domain.IEntity[int64], int64] = (*application[domain.IEntity[int64], int64])(nil)

// New 以资源授权与受控约束通道装饰 Application。
//
// 构造期校验（全部 fail-fast，把配置问题暴露在启动期）：
//   - app / authorizer 非空；
//   - EntityType 非空（否则约束无法定向投放）；
//   - Policy 中权限码合法；
//   - app 提供 crud.IRepositoryProvider 装配能力；
//   - 底层仓储已声明范围列（防静默降级）。
//
// crud.IConfigProvider 可提供授权前的批量上限；未提供时使用默认 1000。
func New[T domain.IEntity[ID], ID comparable](
	app crud.IApplication[T, ID],
	authorizer authscoped.IAuthorizer,
	config Config,
) (crud.IApplication[T, ID], error) {
	if app == nil {
		return nil, errors.NewCode(errors.InvalidInput, "application cannot be nil")
	}
	if authorizer == nil {
		return nil, errors.NewCode(errors.InvalidInput, "authorizer cannot be nil")
	}
	if config.EntityType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "entity type is required to target write constraints")
	}
	if err := config.Policy.Validate(); err != nil {
		return nil, err
	}
	provider, ok := app.(crud.IRepositoryProvider[T, ID])
	if !ok {
		return nil, errors.NewCode(errors.Unsupported, "L3 assembly requires IRepositoryProvider; apply scoped before action")
	}
	repo := provider.Repository()
	if err := requireScopeDeclaration[T, ID](repo, config.EntityType); err != nil {
		return nil, err
	}
	core := &application[T, ID]{inner: app, repository: repo, authorizer: authorizer, config: config}
	core.maxBatchSize = crud.DefaultServiceConfig().MaxBatchSize
	if provider, ok := app.(crud.IConfigProvider); ok {
		if cfg := provider.Config(); cfg != nil && cfg.MaxBatchSize > 0 {
			core.maxBatchSize = cfg.MaxBatchSize
		}
	}
	return composeCapabilities[T, ID](core, app), nil
}

// composeCapabilities 探测内层可选能力，为每项挂上资源授权后组合外壳。
func composeCapabilities[T domain.IEntity[ID], ID comparable](
	core *application[T, ID],
	inner crud.IApplication[T, ID],
) crud.IApplication[T, ID] {
	detected := capability.Detect[T, ID](inner)
	parts := capability.Parts[T, ID]{Core: core, Capabilities: detected.Capabilities}
	if detected.Batch != nil {
		parts.Batch = &batchWriter[T, ID]{app: core, inner: detected.Batch}
	}
	if detected.Audited != nil {
		parts.Audited = &auditedSurface[T, ID]{app: core, inner: detected.Audited}
	}
	return capability.Compose[T, ID](parts)
}
