package governance_test

import (
	"context"
	"testing"
	"time"

	"gochen/auth/scoped"

	"gochen/auth/governance"
	"gochen/errors"
	"gochen/testkit/require"
)

func allowBase() scoped.IAuthorizer {
	return stubAuthorizer{decision: scoped.Allow(scoped.Resource{
		Kind: "order", ID: "1", ManagedScopeID: 101,
	})}
}

type stubAuthorizer struct {
	decision scoped.Decision
	err      error
	calls    *int
}

func (s stubAuthorizer) Authorize(context.Context, string, ...any) (scoped.Decision, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.decision, s.err
}

func (s stubAuthorizer) Require(ctx context.Context, action string, targets ...any) error {
	decision, err := s.Authorize(ctx, action, targets...)
	if err != nil {
		return err
	}
	if !decision.IsAllowed() {
		return errors.NewCode(errors.Forbidden, "denied")
	}
	return nil
}

type recordingAudit struct {
	entries []governance.AuthzLogEntry
	err     error
}

func (r *recordingAudit) SaveAuthzLogEntry(_ context.Context, entry governance.AuthzLogEntry) error {
	r.entries = append(r.entries, entry)
	return r.err
}

func (r *recordingAudit) ListAuthzLogEntries(context.Context, governance.AuthzLogEntryType, int) ([]governance.AuthzLogEntry, error) {
	return r.entries, nil
}

func TestWrapRejectsNilBase(t *testing.T) {
	_, err := governance.Wrap(nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 不启用任何治理能力时构造期拒绝，避免生成无效果的 L4 空壳。
func TestWrapRejectsEmptyGovernance(t *testing.T) {
	base := allowBase()
	_, err := governance.Wrap(base)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestWrapRejectsNilDependencies(t *testing.T) {
	_, err := governance.Wrap(allowBase(), governance.WithClock(nil))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = governance.Wrap(allowBase(), governance.WithIDGenerator(nil))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// --- 快照：默认 fail-closed ---

func TestSnapshotFailureDeniesByDefault(t *testing.T) {
	calls := 0
	base := stubAuthorizer{decision: scoped.Allow(scoped.Resource{Kind: "order", ID: "1"}), calls: &calls}
	wrapped, err := governance.Wrap(base, governance.WithSnapshot(
		governance.PolicySnapshotFunc(func(context.Context) (governance.PolicySnapshot, error) {
			return governance.PolicySnapshot{}, errors.NewCode(errors.ServiceUnavailable, "snapshot store down")
		})))
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, decision.IsAllowed())
	require.Equal(t, 0, calls, "快照不可用时不得触达底层判定")
}

func TestSnapshotFailureCanBeLenient(t *testing.T) {
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(governance.PolicySnapshotFunc(func(context.Context) (governance.PolicySnapshot, error) {
			return governance.PolicySnapshot{}, errors.NewCode(errors.ServiceUnavailable, "down")
		})),
		governance.WithLenientSnapshot())
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.True(t, decision.IsAllowed())
}

// 快照版本必须传播到决策上，供审计与下游元数据复用。
func TestSnapshotVersionPropagatesToDecision(t *testing.T) {
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(governance.PolicySnapshotFunc(func(context.Context) (governance.PolicySnapshot, error) {
			return governance.PolicySnapshot{Key: "k", Version: " v42 "}, nil
		})))
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.Equal(t, "v42", decision.SnapshotVersion)
}

// --- 审计：默认 fail-open，可切严格 ---

func TestAuditRecordsDecision(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(), governance.WithAudit(store))
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.True(t, decision.IsAllowed())

	require.Equal(t, 1, len(store.entries))
	entry := store.entries[0]
	require.Equal(t, governance.AuthzLogEntryTypeDecision, entry.Type)
	require.Equal(t, "order:api:read", entry.Action)
	require.Equal(t, "allow", entry.Effect)
	require.Equal(t, 1, len(entry.Resources))
	require.Equal(t, int64(101), entry.Resources[0].ManagedScopeID)
	require.NotEmpty(t, entry.DecisionID, "决策 ID 必须补齐，便于串联写入日志")
}

func TestAuditFailureIsFailOpenByDefault(t *testing.T) {
	store := &recordingAudit{err: errors.NewCode(errors.ServiceUnavailable, "audit down")}
	wrapped, err := governance.Wrap(allowBase(), governance.WithAudit(store))
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err, "审计不可用不应阻塞授权")
	require.True(t, decision.IsAllowed())
}

func TestStrictAuditFailureDenies(t *testing.T) {
	store := &recordingAudit{err: errors.NewCode(errors.ServiceUnavailable, "audit down")}
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithAudit(store), governance.WithStrictAudit())
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, decision.IsAllowed())
}

// deny 决策同样必须留痕。
func TestAuditRecordsDenyDecision(t *testing.T) {
	store := &recordingAudit{}
	base := stubAuthorizer{decision: scoped.Deny(scoped.ReasonOutOfScope)}
	wrapped, err := governance.Wrap(base, governance.WithAudit(store))
	require.NoError(t, err)

	_, _ = wrapped.Authorize(context.Background(), "order:api:read")
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "deny", store.entries[0].Effect)
	require.Equal(t, scoped.ReasonOutOfScope, store.entries[0].ReasonCode)
}

// Wrap 交付的审计条目必须已脱敏：任何 store（含自定义实现）都不会收到
// 原始 TenantID / OwnerID 这类可用于横向关联的标识。
func TestAuditEntriesAreSanitizedByWrap(t *testing.T) {
	store := &recordingAudit{}
	base := stubAuthorizer{decision: scoped.Allow(scoped.Resource{
		Kind: "order", ID: "1", ManagedScopeID: 101, TenantID: "tenant-a", OwnerID: "owner-a",
	})}
	wrapped, err := governance.Wrap(base, governance.WithAudit(store))
	require.NoError(t, err)

	_, _ = wrapped.Authorize(context.Background(), "order:api:read")
	require.Equal(t, 1, len(store.entries))
	require.Empty(t, store.entries[0].Resources[0].TenantID)
	require.Empty(t, store.entries[0].Resources[0].OwnerID)
	require.Equal(t, int64(101), store.entries[0].Resources[0].ManagedScopeID,
		"脱敏只剔除横向关联标识，授权事实必须保留")
}

// 合规场景经 WithResourceLogSanitizer 注入自定义脱敏器，可保留完整字段。
func TestAuditSanitizerCanKeepIdentifiers(t *testing.T) {
	store := &recordingAudit{}
	base := stubAuthorizer{decision: scoped.Allow(scoped.Resource{
		Kind: "order", ID: "1", ManagedScopeID: 101, TenantID: "tenant-a", OwnerID: "owner-a",
	})}
	wrapped, err := governance.Wrap(base, governance.WithAudit(store))
	require.NoError(t, err)

	ctx, err := governance.WithResourceLogSanitizer(context.Background(),
		governance.ResourceLogSanitizerFunc(func(_ context.Context, resource governance.AuthzLoggedResource) governance.AuthzLoggedResource {
			return resource
		}))
	require.NoError(t, err)

	_, _ = wrapped.Authorize(ctx, "order:api:read")
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "tenant-a", store.entries[0].Resources[0].TenantID)
	require.Equal(t, "owner-a", store.entries[0].Resources[0].OwnerID)
}

// 审计条目携带快照诊断：SnapshotKey 固定，CacheHit 反映本次判定是否命中 TTL 缓存。
func TestAuditRecordsSnapshotDiagnostics(t *testing.T) {
	snapshotStore := &gatedSnapshotStore{snap: &governance.PolicySnapshot{Key: "k", Version: "v1"}}
	mc := &manualClock{now: time.Unix(0, 0)}
	snapshot, err := governance.NewStoreBackedSnapshot(snapshotStore, "k", time.Minute,
		governance.WithSnapshotClock(mc))
	require.NoError(t, err)
	store := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(snapshot), governance.WithAudit(store))
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "v1", store.entries[0].SnapshotVersion)
	require.Equal(t, "k", store.entries[0].SnapshotKey)
	require.False(t, store.entries[0].CacheHit, "首次判定必须回源加载快照")

	_, err = wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.Equal(t, 2, len(store.entries))
	require.True(t, store.entries[1].CacheHit, "TTL 内再次判定应命中缓存")
}

// --- 指标：静默降级，永不影响授权 ---

func TestMetricsFailureNeverAffectsAuthorization(t *testing.T) {
	wrapped, err := governance.Wrap(allowBase(), governance.WithMetrics(
		governance.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {
			panic("metrics backend exploded")
		})))
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err, "指标实现 panic 不得影响授权")
	require.True(t, decision.IsAllowed())
}

func TestMetricsRecordsEffectAndAction(t *testing.T) {
	var gotAction, gotEffect string
	wrapped, err := governance.Wrap(allowBase(), governance.WithMetrics(
		governance.AuthzMetricsFunc(func(_ context.Context, action, effect string, _ time.Duration) {
			gotAction, gotEffect = action, effect
		})))
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.Equal(t, "order:api:read", gotAction)
	require.Equal(t, "allow", gotEffect)
}

// --- 调用链顺序：Metrics(Log(Snapshot(base))) ---

// 审计记录必须已带快照版本——证明 Snapshot 在 Log 内层。
func TestGovernanceChainOrderSnapshotInsideAudit(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(),
		governance.WithSnapshot(governance.PolicySnapshotFunc(func(context.Context) (governance.PolicySnapshot, error) {
			return governance.PolicySnapshot{Version: "v7"}, nil
		})),
		governance.WithAudit(store),
		governance.WithMetrics(governance.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {})),
	)
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), "order:api:read")
	require.NoError(t, err)
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "v7", store.entries[0].SnapshotVersion,
		"审计条目必须已带快照版本，说明 Snapshot 位于 Log 内层")
}

// Require 入口同样经过治理链（不能只有 Authorize 被治理）。
func TestRequireGoesThroughGovernance(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := governance.Wrap(allowBase(), governance.WithAudit(store))
	require.NoError(t, err)

	require.NoError(t, wrapped.Require(context.Background(), "order:api:read"))
	require.Equal(t, 1, len(store.entries), "Require 也必须留痕")
}

func TestRequireDeniesOnDenyDecision(t *testing.T) {
	base := stubAuthorizer{decision: scoped.Deny(scoped.ReasonOutOfScope)}
	wrapped, err := governance.Wrap(base, governance.WithMetrics(
		governance.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {})))
	require.NoError(t, err)

	err = wrapped.Require(context.Background(), "order:api:read")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}
