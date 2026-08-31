package action

import (
	"context"

	"gochen/errors"
)

// IActionChecker 判断当前主体能否执行指定动作。
//
// 实现方负责从 ctx 解析主体（由 Transport 的 AuthN 中间件写入 contextx），
// 本接口不传递主体参数，以保证 HTTP / CommandBus / Worker / CLI 全入口签名一致。
//
// 错误契约：
//   - 主体缺失/未认证 → errors.Unauthorized
//   - 主体已认证但无该动作权限 → errors.Forbidden
//   - 动作码非法 → errors.InvalidInput
type IActionChecker interface {
	RequireAction(ctx context.Context, action string) error
}

// ActionCheckerFunc 允许用函数直接实现 IActionChecker。
type ActionCheckerFunc func(ctx context.Context, action string) error

// RequireAction 校验当前主体是否具备指定动作权限。
func (f ActionCheckerFunc) RequireAction(ctx context.Context, action string) error {
	if f == nil {
		return errors.NewCode(
			errors.InvalidInput,
			"action checker function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx, action)
}

// RequireAction 以 fail-closed 方式执行一次动作校验。
//
// checker 为 nil 或动作码非法时直接拒绝，绝不放行。
func RequireAction(ctx context.Context, checker IActionChecker, code string) error {
	if checker == nil {
		return errors.NewCode(errors.Forbidden, "action checker is required").
			WithContext("action", code)
	}
	if !IsValidCode(code) {
		return errors.NewCode(errors.InvalidInput, "invalid action code").
			WithContext("action", code)
	}
	return checker.RequireAction(ctx, code)
}
