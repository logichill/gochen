package httpx

import (
	"strings"

	"gochen/errors"
)

// IPermissionChecker 权限检查器接口。
//
// 说明：
// - 由业务层实现，用于检查用户是否具有指定权限或角色；
// - RouteBuilder 在注册路由时自动注入权限检查中间件。
type IPermissionChecker interface {
	// HasPermission 检查当前用户是否具有指定权限
	HasPermission(ctx IContext, permission string) bool
	// HasRole 检查当前用户是否具有指定角色
	HasRole(ctx IContext, role string) bool
	// HasAnyPermission 检查当前用户是否具有任一权限
	HasAnyPermission(ctx IContext, permissions []string) bool
	// HasAnyRole 检查当前用户是否具有任一角色
	HasAnyRole(ctx IContext, roles []string) bool
}

func normalizeRouteRequirements(kind string, values []string) ([]string, errors.IAppError) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "route requirement cannot be blank").
			WithContext("requirement", kind)
	}
	return normalized, nil
}

// PermissionMiddleware 创建权限检查中间件。
//
// 说明：
// - 用于需要单独使用权限检查的场景；
// - RouteBuilder 内部已集成权限检查。
func PermissionMiddleware(checker IPermissionChecker, permissions ...string) Middleware {
	if isNilLike(checker) {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "permission checker is nil")
		}
	}
	normalized, err := normalizeRouteRequirements("permission", permissions)
	if err != nil {
		return func(IContext, func() error) error {
			return err
		}
	}
	if len(normalized) == 0 {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "permission middleware requires at least one permission")
		}
	}
	permissions = normalized
	return func(ctx IContext, next func() error) error {
		if !checker.HasAnyPermission(ctx, permissions) {
			return errors.NewCode(errors.Forbidden, "permission denied").
				WithContext("required_permissions", permissions)
		}
		return next()
	}
}

// RoleMiddleware 创建角色检查中间件。
func RoleMiddleware(checker IPermissionChecker, roles ...string) Middleware {
	if isNilLike(checker) {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "role checker is nil")
		}
	}
	normalized, err := normalizeRouteRequirements("role", roles)
	if err != nil {
		return func(IContext, func() error) error {
			return err
		}
	}
	if len(normalized) == 0 {
		return func(IContext, func() error) error {
			return errors.NewCode(errors.InvalidInput, "role middleware requires at least one role")
		}
	}
	roles = normalized
	return func(ctx IContext, next func() error) error {
		if !checker.HasAnyRole(ctx, roles) {
			return errors.NewCode(errors.Forbidden, "role required").
				WithContext("required_roles", roles)
		}
		return next()
	}
}
