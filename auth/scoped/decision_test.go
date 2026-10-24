package scoped_test

import (
	"context"
	"testing"

	"gochen/auth/scoped"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestDecision_AllowedFor(t *testing.T) {
	resource := scoped.Resource{Kind: "order", ID: "1"}

	tests := []struct {
		name        string
		decision    scoped.Decision
		targetCount int
		want        bool
	}{
		{
			name:        "allow with resource permits targeted access",
			decision:    scoped.Allow(resource),
			targetCount: 1,
			want:        true,
		},
		{
			name:        "allow without resource denies targeted access",
			decision:    scoped.Allow(),
			targetCount: 1,
			want:        false,
		},
		{
			name:        "deny denies targeted access",
			decision:    scoped.Deny(scoped.ReasonActionDenied),
			targetCount: 1,
			want:        false,
		},
		{
			name:        "allow without resource permits action-level access",
			decision:    scoped.Allow(),
			targetCount: 0,
			want:        true,
		},
		{
			name:        "allow with resource permits action-level access",
			decision:    scoped.Allow(resource),
			targetCount: 0,
			want:        true,
		},
		{
			name:        "deny denies action-level access",
			decision:    scoped.Deny(scoped.ReasonActionDenied),
			targetCount: 0,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.decision.AllowedFor(tt.targetCount))
		})
	}
}

// --- 零目标 Require 链路守护（审计条目 P2#6 / fingerprint 9a4cca4e）---
//
// 零目标 Require（动作级鉴权）的判定由 IsAllowed 放宽为 IsActionAllowed：
// 评估器返回 allow 且授权资源为空时，无目标调用放行，有目标调用仍拒绝。
// 本组测试通过公开决策入口（Authorize/Require）钉住该有意契约，
// 防止后续改动在无人察觉的情况下回退或进一步放宽。
//
// 契约详见 docs/SPEC-scoped-zero-target-require.md。

// testEvaluator 按 action 返回固定决策：
//   - allowAction：回显解析出的资源（零目标时即为空资源 allow）；
//   - emptyAllowAction：无论解析结果如何都返回空资源 allow；
//   - 其余 action：返回 deny。
func testEvaluator(action string) scoped.EvaluatorFunc {
	switch action {
	case "order:list":
		return func(_ context.Context, _ string, resources []scoped.Resource) (scoped.Decision, error) {
			return scoped.Allow(resources...), nil
		}
	case "order:empty-allow":
		return func(_ context.Context, _ string, _ []scoped.Resource) (scoped.Decision, error) {
			return scoped.Allow(), nil
		}
	default:
		return func(_ context.Context, _ string, _ []scoped.Resource) (scoped.Decision, error) {
			return scoped.Deny(scoped.ReasonActionDenied), nil
		}
	}
}

func newTestAuthorizer(t *testing.T, action string) *scoped.Authorizer {
	t.Helper()

	resolver := scoped.ResourceResolverFunc(func(target any) (scoped.Resource, bool) {
		if resource, ok := target.(scoped.Resource); ok {
			return resource, true
		}
		return scoped.Resource{}, false
	})
	authorizer, err := scoped.NewAuthorizer(resolver, testEvaluator(action))
	require.NoError(t, err)
	return authorizer
}

// 零目标 + 评估器 allow（无授权资源）→ 放行。
// 这是本次有意放宽的核心契约：HEAD 基线下该输入会被 Forbidden 拒绝。
func TestRequireZeroTargetAllowsActionLevelAllow(t *testing.T) {
	authorizer := newTestAuthorizer(t, "order:list")

	require.NoError(t, authorizer.Require(context.Background(), "order:list"))
}

// 零目标 + 评估器 deny → Forbidden，动作级判定同样不豁免 deny。
func TestRequireZeroTargetDeniesActionDeny(t *testing.T) {
	authorizer := newTestAuthorizer(t, "order:delete")

	err := authorizer.Require(context.Background(), "order:delete")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 有目标 + 评估器 allow 且授权资源非空 → 放行，判据不受放宽影响。
func TestRequireWithTargetStillAllowsExplicitResource(t *testing.T) {
	authorizer := newTestAuthorizer(t, "order:list")

	err := authorizer.Require(context.Background(), "order:list",
		scoped.Resource{Kind: "order", ID: "1", ManagedScopeID: 7})
	require.NoError(t, err)
}

// 有目标 + 评估器 allow 但授权资源为空 → Forbidden。
// 放宽只作用于零目标场景；已知目标仍必须显式授权资源（IsAllowed 判据不变）。
func TestRequireWithTargetStillDeniesEmptyAllow(t *testing.T) {
	authorizer := newTestAuthorizer(t, "order:empty-allow")

	err := authorizer.Require(context.Background(), "order:empty-allow",
		scoped.Resource{Kind: "order", ID: "1"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// Authorize 决策口同样暴露零目标契约：空资源 allow 决策原样返回，
// 由调用方按目标数量选择判据（AllowedFor），不在决策层偷偷拒绝。
func TestAuthorizeZeroTargetReturnsActionLevelDecision(t *testing.T) {
	authorizer := newTestAuthorizer(t, "order:list")

	decision, err := authorizer.Authorize(context.Background(), "order:list")
	require.NoError(t, err)
	require.Equal(t, scoped.EffectAllow, decision.Effect)
	require.Empty(t, decision.AuthorizedResources)
	require.True(t, decision.AllowedFor(0))
	require.False(t, decision.AllowedFor(1))
}
