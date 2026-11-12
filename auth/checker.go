package auth

import (
	"context"
	"gochen/auth/action"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// IActionChecker 检查可信上下文中的主体权限；缺身份返回 Unauthorized，缺权限返回 Forbidden。
type IActionChecker interface {
	RequireAction(context.Context, string) error
}

// ActionCheckerFunc 将函数接入动作检查端口。
type ActionCheckerFunc func(context.Context, string) error

// RequireAction 执行动作检查。
func (f ActionCheckerFunc) RequireAction(ctx context.Context, code string) error {
	if f == nil {
		return errors.NewCode(errors.InvalidInput, "action checker function cannot be nil")
	}
	return f(ctx, code)
}

// RequireAction 验证权限码并执行动作检查。
func RequireAction(ctx context.Context, checker IActionChecker, code string) error {
	if depcheck.IsNil(checker) {
		return errors.NewCode(errors.Forbidden, "action checker is required").WithContext("action", code)
	}
	if !action.IsValidCode(code) {
		return errors.NewCode(errors.InvalidInput, "invalid action code").WithContext("action", code)
	}
	return checker.RequireAction(ctx, code)
}
