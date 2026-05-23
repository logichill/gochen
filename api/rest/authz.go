package rest

import (
	"context"
	"strings"

	appaudited "gochen/app/audited"
	appcrud "gochen/app/crud"
	auth "gochen/auth"
	"gochen/domain/access"
	"gochen/errors"
	"gochen/httpx"
)

type writeConstraintSupportValidator interface {
	ValidateWriteConstraintSupport() error
}

func (rb *RouteBuilder[T, ID]) authzConfig() *AuthorizationConfig {
	if rb == nil || rb.config == nil {
		return nil
	}
	return rb.config.Authorization
}

// writeConstraintWriter 为需要写约束的单条写操作选择适配器。
//
// 它优先复用框架内置 Application 的标准 writer；只有在 service 自己实现
// 约束写接口时，才退回到 service 提供的实现。
func (rb *RouteBuilder[T, ID]) writeConstraintWriter() (appcrud.IWriteConstraintWriter[T, ID], bool) {
	if app, ok := any(rb.service).(*appcrud.Application[T, ID]); ok {
		return appcrud.NewWriteConstraintWriter(app), true
	}
	if app, ok := any(rb.service).(*appaudited.Application[T, ID]); ok {
		return appaudited.NewWriteConstraintWriter(app), true
	}
	writer, ok := any(rb.service).(appcrud.IWriteConstraintWriter[T, ID])
	return writer, ok
}

// batchWriter 为批量写路由选择普通 batch writer。
//
// 当批量授权未启用时，路由层仍可能需要一个不带写约束的批量写适配器。
func (rb *RouteBuilder[T, ID]) batchWriter() (appcrud.IBatchWriter[T, ID], bool) {
	if app, ok := any(rb.service).(*appcrud.Application[T, ID]); ok {
		return appcrud.NewBatchWriter(app), true
	}
	if app, ok := any(rb.service).(*appaudited.Application[T, ID]); ok {
		return appaudited.NewBatchWriter(app), true
	}
	writer, ok := any(rb.service).(appcrud.IBatchWriter[T, ID])
	return writer, ok
}

// batchWriteConstraintWriter 为批量写路由选择带写约束的 writer。
//
// 只有 create/update/delete 三类批量写都受授权控制时，路由层才会使用它。
func (rb *RouteBuilder[T, ID]) batchWriteConstraintWriter() (appcrud.IWriteConstraintBatchWriter[T, ID], bool) {
	if app, ok := any(rb.service).(*appcrud.Application[T, ID]); ok {
		return appcrud.NewWriteConstraintWriter(app), true
	}
	if app, ok := any(rb.service).(*appaudited.Application[T, ID]); ok {
		return appaudited.NewWriteConstraintWriter(app), true
	}
	writer, ok := any(rb.service).(appcrud.IWriteConstraintBatchWriter[T, ID])
	return writer, ok
}

// hasWriteAuthorization 判断当前路由是否为 create/update/delete 配置了权限。
func (rb *RouteBuilder[T, ID]) hasWriteAuthorization() bool {
	cfg := rb.authzConfig()
	if cfg == nil {
		return false
	}
	perms := cfg.Permissions
	return (rb.config.Routing.EnableCreate && strings.TrimSpace(perms.Create) != "") ||
		(rb.config.Routing.EnableUpdate && strings.TrimSpace(perms.Update) != "") ||
		(rb.config.Routing.EnableDelete && strings.TrimSpace(perms.Delete) != "")
}

// validateAuthorizationConfig 校验授权配置与底层 service/repository 能力是否匹配。
//
// 这一步会在路由注册前尽早失败，避免把“声明了写授权但 service 不支持约束写”
// 之类的问题拖到运行时才暴露。
func (rb *RouteBuilder[T, ID]) validateAuthorizationConfig() error {
	cfg := rb.authzConfig()
	if cfg == nil {
		return nil
	}
	if cfg.Authorizer == nil {
		return errors.NewCode(errors.InvalidInput, "RouteConfig.Authorization.Authorizer is required")
	}
	if rb.hasWriteAuthorization() {
		if _, ok := rb.writeConstraintWriter(); !ok {
			return errors.NewCode(errors.InvalidInput, "authz-enabled write routes require a built-in constrained writer or service-specific constraint writer")
		}
		if validator, ok := rb.service.(writeConstraintSupportValidator); ok && validator != nil {
			if err := validator.ValidateWriteConstraintSupport(); err != nil {
				return err
			}
		}
		if provider, ok := rb.repositoryProvider(); ok {
			repo := provider.Repository()
			if validator, ok := any(repo).(writeConstraintSupportValidator); ok && validator != nil {
				if err := validator.ValidateWriteConstraintSupport(); err != nil {
					return err
				}
			}
		}
	}
	if rb.config != nil && rb.hasEnabledBatchRoutes() && rb.hasWriteAuthorization() {
		if _, ok := rb.batchWriteConstraintWriter(); !ok {
			return errors.NewCode(errors.InvalidInput, "authz-enabled batch write routes require a built-in constrained batch writer or service-specific constraint writer")
		}
	}
	return nil
}

// authorize 在调用 Authorizer 之前补齐请求上下文，并强制要求决策结果为 allow。
func (rb *RouteBuilder[T, ID]) authorize(
	c httpx.IContext,
	ctx context.Context,
	permission string,
	targets ...any,
) (context.Context, auth.AuthzDecision, error) {
	cfg := rb.authzConfig()
	permission = strings.TrimSpace(permission)
	if cfg == nil || permission == "" {
		return ctx, auth.AllowDecision(), nil
	}
	var err error
	if cfg.Consistency != auth.ConsistencyModeUnspecified {
		ctx, err = auth.WithConsistencyMode(ctx, cfg.Consistency)
		if err != nil {
			return ctx, auth.AuthzDecision{}, err
		}
	}
	if cfg.HighRisk {
		ctx, err = auth.WithHighRiskAuthorization(ctx)
		if err != nil {
			return ctx, auth.AuthzDecision{}, err
		}
	}
	ctx, _, err = auth.BindAuthzEvalContextOrEmpty(ctx)
	if err != nil {
		return ctx, auth.AuthzDecision{}, err
	}
	ctx = rb.syncRequestContext(c, ctx)
	decision, err := cfg.Authorizer.Authorize(ctx, permission, targets...)
	if err != nil {
		return ctx, auth.AuthzDecision{}, err
	}
	if err := decision.RequireAllow(); err != nil {
		return ctx, auth.AuthzDecision{}, err
	}
	return ctx, decision, nil
}

// syncRequestContext 把注入了 authz 运行时状态的新 ctx 回写到 HTTP 请求对象。
//
// 后续 handler 若继续从 request context 读取 principal、scope 或 snapshot 信息，
// 应该看到的是授权链路已经补齐后的上下文。
func (rb *RouteBuilder[T, ID]) syncRequestContext(c httpx.IContext, ctx context.Context) context.Context {
	if c == nil || ctx == nil {
		return ctx
	}
	reqCtx := c.RequestContext()
	if reqCtx == nil {
		return ctx
	}
	reqCtx = reqCtx.WithContext(ctx)
	c.SetContext(reqCtx)
	return reqCtx
}

// createPermission 返回 create 路由使用的权限标识。
func (rb *RouteBuilder[T, ID]) createPermission() string {
	cfg := rb.authzConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Permissions.Create)
}

// updatePermission 返回 update 路由使用的权限标识。
func (rb *RouteBuilder[T, ID]) updatePermission() string {
	cfg := rb.authzConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Permissions.Update)
}

// deletePermission 返回 delete 路由使用的权限标识。
func (rb *RouteBuilder[T, ID]) deletePermission() string {
	cfg := rb.authzConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Permissions.Delete)
}

// getPermission 返回 get 路由使用的权限标识。
func (rb *RouteBuilder[T, ID]) getPermission() string {
	cfg := rb.authzConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Permissions.Get)
}

// listPermission 返回 list 路由使用的权限标识。
func (rb *RouteBuilder[T, ID]) listPermission() string {
	cfg := rb.authzConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Permissions.List)
}

// rejectBatchAuthorization 当前不做额外处理，保留这个方法只是为了集中放置未来的批量授权分支。
func (rb *RouteBuilder[T, ID]) rejectBatchAuthorization() error {
	return nil
}

// batchCreatePermission 返回 batch create 路由复用的权限标识。
func (rb *RouteBuilder[T, ID]) batchCreatePermission() string {
	return rb.createPermission()
}

// batchUpdatePermission 返回 batch update 路由复用的权限标识。
func (rb *RouteBuilder[T, ID]) batchUpdatePermission() string {
	return rb.updatePermission()
}

// batchDeletePermission 返回 batch delete 路由复用的权限标识。
func (rb *RouteBuilder[T, ID]) batchDeletePermission() string {
	return rb.deletePermission()
}

// requiresPlainBatchWriter 判断批量写是否只能退回到无约束 writer。
//
// 只要三类批量写里有任一权限未配置，路由层就不能假设整批写入都走约束写接口。
func (rb *RouteBuilder[T, ID]) requiresPlainBatchWriter() bool {
	return (rb.config.Routing.EnableCreate && rb.batchCreatePermission() == "") ||
		(rb.config.Routing.EnableUpdate && rb.batchUpdatePermission() == "") ||
		(rb.config.Routing.EnableDelete && rb.batchDeletePermission() == "")
}

// resourceBoundaryRepository 返回支持“按资源 ID 反查边界”的 repository。
func (rb *RouteBuilder[T, ID]) resourceBoundaryRepository() (access.IResourceBoundaryRepository[T, ID], bool) {
	if rb == nil || rb.service == nil {
		return nil, false
	}
	if repo, ok := rb.service.(access.IResourceBoundaryRepository[T, ID]); ok {
		return repo, true
	}
	provider, ok := rb.repositoryProvider()
	if !ok {
		return nil, false
	}
	repo, ok := provider.Repository().(access.IResourceBoundaryRepository[T, ID])
	return repo, ok
}

// resolveResourceBoundary 通过资源 ID 提前解析受影响资源的 scope 边界。
//
// 对 update/delete 这类先知道 ID、后进入 service 的路由，框架依赖这个步骤把
// managed scope 放进上下文，从而让后续写约束与审计落到正确的边界上。
func (rb *RouteBuilder[T, ID]) resolveResourceBoundary(ctx context.Context, id ID) (access.ResourceBoundary, bool, error) {
	repo, ok := rb.resourceBoundaryRepository()
	if !ok {
		return access.ResourceBoundary{}, false, nil
	}
	resource, err := repo.ResolveResourceByID(ctx, id)
	if err != nil {
		return access.ResourceBoundary{}, true, err
	}
	return resource, true, nil
}

// contextForResolvedResource 把已解析出的资源边界投影为 auth.DataScope。
func (rb *RouteBuilder[T, ID]) contextForResolvedResource(ctx context.Context, resource access.ResourceBoundary) (context.Context, error) {
	if resource.ManagedScopeID == 0 {
		return ctx, nil
	}
	return auth.WithDataScope(ctx, auth.DataScope{
		ActiveScopeID:   resource.ManagedScopeID,
		VisibleScopeIDs: []int64{resource.ManagedScopeID},
		Mode:            auth.ScopeModeScoped,
	})
}

// contextForResourceID 为“先拿到资源 ID 再写”的路由构造带资源边界的上下文。
func (rb *RouteBuilder[T, ID]) contextForResourceID(ctx context.Context, id ID) (context.Context, access.ResourceBoundary, bool, error) {
	resource, ok, err := rb.resolveResourceBoundary(ctx, id)
	if err != nil || !ok {
		return ctx, resource, ok, err
	}
	scopedCtx, err := rb.contextForResolvedResource(ctx, resource)
	if err != nil {
		return nil, access.ResourceBoundary{}, true, err
	}
	return scopedCtx, resource, true, nil
}
