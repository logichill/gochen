package auth_test

import (
	"context"
	"testing"
	"time"

	"gochen/auth"
	"gochen/domain/spec"

	"gochen/errors"
	"gochen/testkit/require"
)

func allowBase() auth.IPolicy {
	return governanceStubPolicy{decision: auth.Decision{Allowed: true, Spec: spec.All()}}
}

type governanceStubPolicy struct {
	decision auth.Decision
	err      error
	calls    *int
}

func (s governanceStubPolicy) Evaluate(context.Context, auth.Request) (auth.Decision, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.decision, s.err
}

type recordingAudit struct {
	entries []auth.AuthzLogEntry
	err     error
}

func (r *recordingAudit) SaveAuthzLogEntry(_ context.Context, entry auth.AuthzLogEntry) error {
	r.entries = append(r.entries, entry)
	return r.err
}

func (r *recordingAudit) ListAuthzLogEntries(context.Context, auth.AuthzLogEntryType, int) ([]auth.AuthzLogEntry, error) {
	return r.entries, nil
}

func TestWrapRejectsNilBase(t *testing.T) {
	_, err := auth.New(auth.Options{Policy: nil})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 零治理配置沿用完整授权执行，适用于直接 Core 接入。
func TestNewSupportsDisabledGovernance(t *testing.T) {
	base := allowBase()
	_, err := auth.New(auth.Options{Policy: base})
	require.NoError(t, err)
}

func TestWrapRejectsNilDependencies(t *testing.T) {
	var c *manualClock
	_, err := auth.New(auth.Options{Policy: allowBase(), Clock: c})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{StrictAudit: true}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// --- 快照：默认 fail-closed ---

func TestSnapshotFailureDeniesByDefault(t *testing.T) {
	calls := 0
	base := governanceStubPolicy{decision: auth.Decision{Allowed: true, Spec: spec.All()}, calls: &calls}
	wrapped, err := auth.New(auth.Options{Policy: base, Governance: auth.GovernanceOptions{Snapshot: auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
		return auth.PolicySnapshot{}, errors.NewCode(errors.ServiceUnavailable, "snapshot store down")
	})}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, decision.Allowed)
	require.Equal(t, 0, calls, "快照不可用时不得触达底层判定")
}

func TestSnapshotFailureCanBeLenient(t *testing.T) {
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Snapshot: auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
		return auth.PolicySnapshot{}, errors.NewCode(errors.ServiceUnavailable, "down")
	}), LenientSnapshot: true}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

// 快照版本必须传播到决策上，供审计与下游元数据复用。
func TestSnapshotVersionPropagatesToDecision(t *testing.T) {
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Snapshot: auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
		return auth.PolicySnapshot{Key: "k", Version: " v42 "}, nil
	})}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.Equal(t, "v42", decision.SnapshotVersion)
}

func TestSnapshotLabelDoesNotBindPolicyEvaluationOrRequest(t *testing.T) {
	ctx := context.Background()
	clock := &manualClock{now: time.Unix(0, 0)}
	snapshotStore := &gatedSnapshotStore{snap: &auth.PolicySnapshot{Key: "k", Version: "v1"}}
	snapshot, err := auth.NewStoreBackedSnapshot(snapshotStore, "k", time.Minute,
		auth.WithSnapshotClock(clock))
	require.NoError(t, err)
	granted := true
	base, err := auth.NewPermissionPolicy(auth.ActionCheckerFunc(func(context.Context, string) error {
		if !granted {
			return errors.NewCode(errors.Forbidden, "permission revoked")
		}
		return nil
	}))
	require.NoError(t, err)
	audit := &recordingAudit{}
	wrapped, err := auth.New(auth.Options{Policy: base, Governance: auth.GovernanceOptions{Snapshot: snapshot, Audit: audit}})
	require.NoError(t, err)
	request := auth.Request{Resource: "order", Action: "read", Permission: "order:api:read"}
	decision, err := wrapped.Authorize(ctx, request)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, "v1", decision.SnapshotVersion)

	// 权限变更立刻影响 checker；版本标签仍来自 TTL 缓存。
	granted = false
	snapshotStore.snap = &auth.PolicySnapshot{Key: "k", Version: "v2"}
	decision, err = wrapped.Authorize(ctx, request)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "v1", decision.SnapshotVersion)
	require.Equal(t, "v1", audit.entries[1].SnapshotVersion)
	require.Equal(t, "deny", audit.entries[1].Effect)

	// 同一 ctx 跨过 TTL 后获得新标签，通用包装器没有请求级冻结。
	clock.advance(time.Minute)
	decision, err = wrapped.Authorize(ctx, request)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "v2", decision.SnapshotVersion)
	require.Equal(t, "v2", audit.entries[2].SnapshotVersion)
	require.Equal(t, 2, snapshotStore.loadCount())
}

// --- 审计：默认 fail-open，可切严格 ---

func TestAuditRecordsDecision(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Audit: store}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	require.Equal(t, 1, len(store.entries))
	entry := store.entries[0]
	require.Equal(t, auth.AuthzLogEntryTypeDecision, entry.Type)
	require.Equal(t, "order:api:read", entry.Action)
	require.Equal(t, "allow", entry.Effect)
	require.Equal(t, 1, len(entry.Resources))
	require.Equal(t, "order", entry.Resources[0].Kind)
	require.Equal(t, "1", entry.Resources[0].ID)
	require.NotEmpty(t, entry.DecisionID, "决策 ID 必须补齐，便于串联写入日志")
}

func TestAuditFailureIsFailOpenByDefault(t *testing.T) {
	store := &recordingAudit{err: errors.NewCode(errors.ServiceUnavailable, "audit down")}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Audit: store}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err, "审计不可用不应阻塞授权")
	require.True(t, decision.Allowed)
}

func TestStrictAuditFailureDenies(t *testing.T) {
	store := &recordingAudit{err: errors.NewCode(errors.ServiceUnavailable, "audit down")}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Audit: store, StrictAudit: true}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, decision.Allowed)
}

// deny 决策同样必须留痕。
func TestAuditRecordsDenyDecision(t *testing.T) {
	store := &recordingAudit{}
	base := governanceStubPolicy{decision: auth.Decision{Reason: "out_of_scope"}}
	wrapped, err := auth.New(auth.Options{Policy: base, Governance: auth.GovernanceOptions{Audit: store}})
	require.NoError(t, err)

	_, _ = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "deny", store.entries[0].Effect)
	require.Equal(t, "out_of_scope", store.entries[0].ReasonCode)
}

// Wrap 交付的审计条目必须已脱敏：任何 store（含自定义实现）都不会收到
// 原始 TenantID / OwnerID 这类可用于横向关联的标识。

// 合规场景经 WithResourceLogSanitizer 注入自定义脱敏器，可保留完整字段。

// 审计条目携带快照诊断：SnapshotKey 固定，CacheHit 反映本次判定是否命中 TTL 缓存。
func TestAuditRecordsSnapshotDiagnostics(t *testing.T) {
	snapshotStore := &gatedSnapshotStore{snap: &auth.PolicySnapshot{Key: "k", Version: "v1"}}
	mc := &manualClock{now: time.Unix(0, 0)}
	snapshot, err := auth.NewStoreBackedSnapshot(snapshotStore, "k", time.Minute,
		auth.WithSnapshotClock(mc))
	require.NoError(t, err)
	store := &recordingAudit{}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Snapshot: snapshot, Audit: store}})
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "v1", store.entries[0].SnapshotVersion)
	require.Equal(t, "k", store.entries[0].SnapshotKey)
	require.False(t, store.entries[0].CacheHit, "首次判定必须回源加载快照")

	_, err = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.Equal(t, 2, len(store.entries))
	require.True(t, store.entries[1].CacheHit, "TTL 内再次判定应命中缓存")
}

// --- 指标：静默降级，永不影响授权 ---

func TestMetricsFailureNeverAffectsAuthorization(t *testing.T) {
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Metrics: auth.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {
		panic("metrics backend exploded")
	})}})
	require.NoError(t, err)

	decision, err := wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err, "指标实现 panic 不得影响授权")
	require.True(t, decision.Allowed)
}

func TestMetricsRecordsEffectAndAction(t *testing.T) {
	var gotAction, gotEffect string
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Metrics: auth.AuthzMetricsFunc(func(_ context.Context, action, effect string, _ time.Duration) {
		gotAction, gotEffect = action, effect
	})}})
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.Equal(t, "order:api:read", gotAction)
	require.Equal(t, "allow", gotEffect)
}

// --- 调用链顺序：Metrics(Log(Snapshot(base))) ---

// 审计记录必须已带快照版本——证明 Snapshot 在 Log 内层。
func TestGovernanceChainOrderSnapshotInsideAudit(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Snapshot: auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
		return auth.PolicySnapshot{Version: "v7"}, nil
	}), Audit: store, Metrics: auth.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {})}})
	require.NoError(t, err)

	_, err = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1"}})
	require.NoError(t, err)
	require.Equal(t, 1, len(store.entries))
	require.Equal(t, "v7", store.entries[0].SnapshotVersion,
		"审计条目必须已带快照版本，说明 Snapshot 位于 Log 内层")
}

// Require 入口同样经过治理链（不能只有 Authorize 被治理）。

func (s governanceStubPolicy) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }

func TestGovernancePreservesAllowPolicyIdentification(t *testing.T) {
	metrics := auth.AuthzMetricsFunc(func(context.Context, string, string, time.Duration) {})
	for _, tc := range []struct {
		name string
		base auth.IPolicy
		want bool
	}{
		{name: "allow", base: auth.AllowPolicy(), want: true},
		{name: "custom policy", base: allowBase(), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped, err := auth.New(auth.Options{Policy: tc.base, Governance: auth.GovernanceOptions{Snapshot: auth.PolicySnapshotFunc(func(context.Context) (auth.PolicySnapshot, error) {
				return auth.PolicySnapshot{Key: "policy", Version: "v1"}, nil
			}), Audit: &recordingAudit{}, Metrics: metrics}})
			require.NoError(t, err)
			require.Equal(t, tc.want, auth.IsAllow(wrapped))
			decision, err := wrapped.Authorize(context.Background(), auth.Request{Permission: "order:api:read"})
			require.NoError(t, err)
			require.True(t, decision.Allowed)
		})
	}
}

func TestGovernanceRejectsTypedNilDependencies(t *testing.T) {
	var base *governanceStubPolicy
	var snapshot auth.PolicySnapshotFunc
	var audit *recordingAudit
	var metrics auth.AuthzMetricsFunc
	var c *manualClock
	for _, tc := range []struct {
		name string
		opts auth.Options
	}{
		{name: "base", opts: auth.Options{Policy: base}},
		{name: "snapshot", opts: auth.Options{Policy: auth.AllowPolicy(), Governance: auth.GovernanceOptions{Snapshot: snapshot}}},
		{name: "audit", opts: auth.Options{Policy: auth.AllowPolicy(), Governance: auth.GovernanceOptions{Audit: audit}}},
		{name: "metrics", opts: auth.Options{Policy: auth.AllowPolicy(), Governance: auth.GovernanceOptions{Metrics: metrics}}},
		{name: "clock", opts: auth.Options{Policy: auth.AllowPolicy(), Clock: c}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := auth.New(tc.opts)
			require.True(t, errors.Is(err, errors.InvalidInput))
		})
	}
}
func TestGovernanceLogsRequestWithoutObjectState(t *testing.T) {
	store := &recordingAudit{}
	wrapped, err := auth.New(auth.Options{Policy: allowBase(), Governance: auth.GovernanceOptions{Audit: store}})
	require.NoError(t, err)
	_, err = wrapped.Authorize(context.Background(), auth.Request{Resource: "order", Action: "read", Permission: "order:api:read", IDs: []string{"1", "2"}})
	require.NoError(t, err)
	require.Equal(t, 2, len(store.entries[0].Resources))
	require.Equal(t, "2", store.entries[0].Resources[1].ID)
	require.Empty(t, store.entries[0].Resources[0].TenantID)
	require.Empty(t, store.entries[0].Resources[0].OwnerID)
}
