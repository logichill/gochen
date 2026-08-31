package action_test

import (
	"context"
	"testing"

	"gochen/auth/action"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestIsValidCode(t *testing.T) {
	valid := []string{
		"order:api:create",
		"ecommerce.product:data:export",
		"statistics-owner:menu:view",
		"ems.costing.price:api:write",
		"order:api:*",
		"*:*:*",
	}
	for _, code := range valid {
		require.True(t, action.IsValidCode(code), code)
	}

	invalid := []string{
		"",
		"api:order",
		"order:create:api:extra",
		"*order:api:create", // 通配不可与其他字符混用
		"order:api:cre*ate",
		"api.order:create",
		"tenant:sub-module.custom:manage",
	}
	for _, code := range invalid {
		require.False(t, action.IsValidCode(code), code)
	}
}

func TestPatternMatches(t *testing.T) {
	require.True(t, action.PatternMatches("order:api:*", "order:api:create"))
	require.True(t, action.PatternMatches("*:*:*", "order:api:create"))
	require.True(t, action.PatternMatches("ORDER:API:*", "order:api:create"))
	require.True(t, action.PatternMatches("ems.costing.price:api:*", "ems.costing.price:api:write"))
	require.True(t, action.PatternMatches("statistics-owner:menu:*", "statistics-owner:menu:view"))

	require.False(t, action.PatternMatches("api:order", "order:api:create"))
	require.False(t, action.PatternMatches("user:api:*", "order:api:create"))
	require.False(t, action.PatternMatches("order:api:read", "order:api:create"))
}

func TestOperationPolicyCodeFor(t *testing.T) {
	policy := action.OperationPolicy{
		Create: "order:api:create",
		Read:   "order:api:read",
	}

	code, ok := policy.CodeFor(action.OpCreate)
	require.True(t, ok)
	require.Equal(t, "order:api:create", code)

	// 未配置的操作必须报告"无配置"，由调用方 fail-closed 拒绝。
	_, ok = policy.CodeFor(action.OpDelete)
	require.False(t, ok)

	_, ok = policy.CodeFor(action.Operation("unknown"))
	require.False(t, ok)
}

func TestOperationPolicyValidate(t *testing.T) {
	require.NoError(t, action.OperationPolicy{
		Create: "order:api:create",
		List:   "order:api:*",
	}.Validate())

	err := action.OperationPolicy{Create: "api:order"}.Validate()
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestRequireActionFailsClosed(t *testing.T) {
	ctx := context.Background()

	// checker 缺失 → 拒绝，绝不放行。
	err := action.RequireAction(ctx, nil, "order:api:create")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))

	granted := action.ActionCheckerFunc(func(context.Context, string) error { return nil })

	// 动作码非法 → 拒绝（即使 checker 会放行）。
	err = action.RequireAction(ctx, granted, "api:order")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	require.NoError(t, action.RequireAction(ctx, granted, "order:api:create"))
}

func TestActionCheckerFuncNil(t *testing.T) {
	var fn action.ActionCheckerFunc
	err := fn.RequireAction(context.Background(), "order:api:create")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
