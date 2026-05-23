package authhttp

import (
	"context"
	"strings"

	auth "gochen/auth"
	"gochen/errors"
	"gochen/httpx"
)

type permissionChecker struct{}

// HasPermission 判断请求上下文中的主体是否具备指定权限。
func HasPermission(ctx httpx.IRequestContext, permission string) bool {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return true
	}
	if ctx == nil {
		return false
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return false
	}
	return principal.AllowsPermission(permission)
}

// HasAnyRole 判断请求上下文中的主体是否命中任一角色。
func HasAnyRole(ctx httpx.IRequestContext, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	if ctx == nil {
		return false
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return false
	}
	for _, need := range required {
		need = strings.TrimSpace(need)
		if need == "" {
			continue
		}
		for _, role := range principal.Roles {
			if strings.EqualFold(strings.TrimSpace(role), need) {
				return true
			}
		}
	}
	return false
}

// HasPermission 适配 `httpx.PermissionChecker` 对当前 HTTP 上下文做权限判断。
func (permissionChecker) HasPermission(ctx httpx.IContext, permission string) bool {
	if ctx == nil {
		return false
	}
	return HasPermission(ctx.RequestContext(), permission)
}

// HasAnyPermission 适配 `httpx.PermissionChecker` 对当前 HTTP 上下文做任一权限判断。
func (permissionChecker) HasAnyPermission(ctx httpx.IContext, permissions []string) bool {
	if len(permissions) == 0 {
		return true
	}
	if ctx == nil {
		return false
	}
	reqCtx := ctx.RequestContext()
	if reqCtx == nil {
		return false
	}
	for _, permission := range permissions {
		if HasPermission(reqCtx, permission) {
			return true
		}
	}
	return false
}

// HasRole 适配 `httpx.PermissionChecker` 对当前 HTTP 上下文做单角色判断。
func (permissionChecker) HasRole(ctx httpx.IContext, role string) bool {
	if ctx == nil {
		return false
	}
	return HasAnyRole(ctx.RequestContext(), role)
}

// HasAnyRole 适配 `httpx.PermissionChecker` 对当前 HTTP 上下文做任一角色判断。
func (permissionChecker) HasAnyRole(ctx httpx.IContext, roles []string) bool {
	if len(roles) == 0 {
		return true
	}
	if ctx == nil {
		return false
	}
	return HasAnyRole(ctx.RequestContext(), roles...)
}

// PermissionMiddleware 构造权限校验中间件，并在通过后绑定权限运行时上下文。
//
// 对高风险权限，这里会额外写入高风险授权标记，供后续业务层或审计层识别。
func PermissionMiddleware(required auth.PermissionSpec) httpx.Middleware {
	requiredPermission := required.Definition()
	if !auth.IsValidPermissionCode(requiredPermission.Code) {
		return func(httpx.IContext, func() error) error {
			return errors.NewCode(errors.Internal, "invalid permission definition")
		}
	}

	base := httpx.PermissionMiddleware(permissionChecker{}, requiredPermission.Code)
	return func(ctx httpx.IContext, next func() error) error {
		reqCtx := ctx.RequestContext()
		if reqCtx == nil {
			return errors.NewCode(errors.Unauthorized, "principal is required")
		}
		if _, ok := auth.PrincipalFromContext(reqCtx); !ok {
			return errors.NewCode(errors.Unauthorized, "principal is required")
		}

		called := false
		err := base(ctx, func() error {
			called = true
			boundCtx, err := bindPermissionRuntime(reqCtx, requiredPermission)
			if err != nil {
				return err
			}
			ctx.SetContext(reqCtx.WithContext(boundCtx))
			return next()
		})
		if err != nil && !called {
			return err
		}
		return err
	}
}

// bindPermissionRuntime 根据权限定义补齐鉴权运行时上下文。
func bindPermissionRuntime(reqCtx httpx.IRequestContext, requiredPermission auth.PermissionDefinition) (context.Context, error) {
	var runtimeCtx context.Context = reqCtx
	switch strings.ToLower(strings.TrimSpace(requiredPermission.RiskLevel)) {
	case string(auth.PermissionRiskHigh), string(auth.PermissionRiskCritical):
		derived, err := auth.WithHighRiskAuthorization(runtimeCtx)
		if err != nil {
			return nil, err
		}
		runtimeCtx = derived
	}
	derived, _, err := auth.BindAuthzEvalContextOrEmpty(runtimeCtx)
	if err != nil {
		return nil, err
	}
	return derived, nil
}
