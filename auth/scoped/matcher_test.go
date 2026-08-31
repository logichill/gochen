package scoped_test

import (
	"strconv"
	"testing"

	"gochen/auth/scoped"
	"gochen/errors"
	"gochen/testkit/require"
)

// --- SplitByTargets：批量约束按目标拆分 ---

func targetsOf(kind string, ids ...string) []scoped.Resource {
	targets := make([]scoped.Resource, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, scoped.Resource{Kind: kind, ID: id})
	}
	return targets
}

func splitResourceIDs(t *testing.T, split []scoped.WriteConstraint) []string {
	t.Helper()
	ids := make([]string, 0, len(split))
	for _, constraint := range split {
		resource, ok := constraint.Single()
		require.True(t, ok, "每个拆分结果必须恰好绑定一个资源")
		ids = append(ids, resource.ResourceID)
	}
	return ids
}

// 拆分结果必须按目标顺序对齐，而不是按约束自身的顺序。
func TestSplitByTargetsMatchesByResourceIDNotOrder(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ResourceID: "3", ManagedScopeID: 103},
		{Kind: "order", ResourceID: "1", ManagedScopeID: 101},
		{Kind: "order", ResourceID: "2", ManagedScopeID: 102},
	}}

	split, err := constraint.SplitByTargets(targetsOf("order", "1", "2", "3"))
	require.NoError(t, err)
	require.Equal(t, 3, len(split))
	require.Equal(t, []string{"1", "2", "3"}, splitResourceIDs(t, split))

	// 范围也必须跟着目标走，串位就等于给某条记录套了别人的范围。
	first, ok := split[0].Single()
	require.True(t, ok)
	require.Equal(t, int64(101), first.ManagedScopeID)
}

// 约束未携带具体 ID 时退化为按 kind 顺序匹配。
func TestSplitByTargetsFallsBackToKindOrder(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ManagedScopeID: 101},
		{Kind: "order", ManagedScopeID: 102},
	}}

	split, err := constraint.SplitByTargets(targetsOf("order", "7", "8"))
	require.NoError(t, err)

	first, ok := split[0].Single()
	require.True(t, ok)
	require.Equal(t, int64(101), first.ManagedScopeID)
	second, ok := split[1].Single()
	require.True(t, ok)
	require.Equal(t, int64(102), second.ManagedScopeID)
}

// 精确匹配优先于 kind 兜底：带 ID 的约束先被目标取走，
// 匹配不到 ID 的目标才去消费无 ID 的那条。
func TestSplitByTargetsPrefersExactMatchOverKindFallback(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ManagedScopeID: 900},
		{Kind: "order", ResourceID: "2", ManagedScopeID: 102},
	}}

	split, err := constraint.SplitByTargets(targetsOf("order", "2", "5"))
	require.NoError(t, err)

	exact, ok := split[0].Single()
	require.True(t, ok)
	require.Equal(t, "2", exact.ResourceID)
	require.Equal(t, int64(102), exact.ManagedScopeID)

	fallback, ok := split[1].Single()
	require.True(t, ok)
	require.Equal(t, "", fallback.ResourceID)
	require.Equal(t, int64(900), fallback.ManagedScopeID)
}

// 同一 ID 有多条约束时按声明顺序逐条取用，不得重复取同一条。
func TestSplitByTargetsConsumesDuplicateResourceIDsInOrder(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ResourceID: "1", ManagedScopeID: 101},
		{Kind: "order", ResourceID: "1", ManagedScopeID: 202},
	}}

	split, err := constraint.SplitByTargets(targetsOf("order", "1", "1"))
	require.NoError(t, err)

	first, ok := split[0].Single()
	require.True(t, ok)
	require.Equal(t, int64(101), first.ManagedScopeID)
	second, ok := split[1].Single()
	require.True(t, ok)
	require.Equal(t, int64(202), second.ManagedScopeID)
}

// 目标未被任何约束覆盖 → Forbidden，绝不放行。
func TestSplitByTargetsRejectsUnauthorizedTarget(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ResourceID: "1"},
		{Kind: "order", ResourceID: "2"},
	}}

	_, err := constraint.SplitByTargets(targetsOf("order", "1", "9"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// kind 不匹配同样不得跨类型借用约束。
func TestSplitByTargetsRejectsForeignKind(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ResourceID: "1"},
	}}

	_, err := constraint.SplitByTargets(targetsOf("invoice", "1"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 数量不符即整批拒绝，避免"部分授权"被当成全量授权。
func TestSplitByTargetsRejectsCountMismatch(t *testing.T) {
	constraint := scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
		{Kind: "order", ResourceID: "1"},
	}}

	_, err := constraint.SplitByTargets(targetsOf("order", "1", "2"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestSplitByTargetsReturnsNilForEmptyTargets(t *testing.T) {
	split, err := scoped.WriteConstraint{}.SplitByTargets(nil)
	require.NoError(t, err)
	require.Equal(t, 0, len(split))
}

// 批量上限规模下仍必须逐条对齐——这条同时兜住索引表实现的等价性。
func TestSplitByTargetsAlignsAtBatchLimitScale(t *testing.T) {
	const size = 1000
	resources := make([]scoped.ResourceConstraint, 0, size)
	ids := make([]string, 0, size)
	for i := 0; i < size; i++ {
		// 约束按逆序声明，强制匹配走 ID 而不是位置。
		id := strconv.Itoa(size - i)
		resources = append(resources, scoped.ResourceConstraint{
			Kind:           "order",
			ResourceID:     id,
			ManagedScopeID: int64(size - i),
		})
		ids = append(ids, strconv.Itoa(i+1))
	}

	split, err := scoped.WriteConstraint{Resources: resources}.SplitByTargets(targetsOf("order", ids...))
	require.NoError(t, err)
	require.Equal(t, size, len(split))
	for i, constraint := range split {
		resource, ok := constraint.Single()
		require.True(t, ok)
		require.Equal(t, ids[i], resource.ResourceID)
		require.Equal(t, int64(i+1), resource.ManagedScopeID)
	}
}
