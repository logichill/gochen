// Package governance 提供 L4 企业治理外壳：策略快照一致性、决策审计留痕与监控。
//
// 设计要点：
//   - 治理能力实现为 **Authorizer Decorator**（包装 scoped.IAuthorizer），
//     而不是包装 Application——不使用时完全不参与编译；
//   - 固化调用链 Metrics(Log(Snapshot(base)))，包装顺序由本包决定，
//     不允许业务层自行拼接（§4.3 风险控制）。
package governance

import (
	"context"
	"strings"
	"time"

	"gochen/auth/scoped"
	"gochen/errors"
)

// ConsistencyMode 表达授权判定对策略新鲜度的要求。
type ConsistencyMode string

const (
	// ConsistencyModeUnspecified 未显式声明，由框架按默认值处理。
	ConsistencyModeUnspecified ConsistencyMode = ""
	// ConsistencyModeStrong 必须基于当前时刻的实时权限事实判定。
	ConsistencyModeStrong ConsistencyMode = "strong"
	// ConsistencyModeBoundedStaleness 允许在受控范围内使用陈旧快照。
	ConsistencyModeBoundedStaleness ConsistencyMode = "bounded_staleness"
)

// ExecutionMetadata 表达一次授权执行链的追踪元数据。
type ExecutionMetadata struct {
	InitiatorID string
	ActorID     string
	RequestID   string
	DecisionID  string
	EventID     string
	JobID       string
}

// PolicySnapshot 表达一次策略快照。
type PolicySnapshot struct {
	Key       string
	Version   string
	Timestamp time.Time
	Metadata  map[string]any
}

// IPolicySnapshot 解析当前授权判定应使用的策略快照。
//
// 快照作用范围由创建者在请求/事务/批处理级别显式固定。
type IPolicySnapshot interface {
	ResolveSnapshot(ctx context.Context) (PolicySnapshot, error)
}

// PolicySnapshotFunc 允许用函数直接实现 IPolicySnapshot。
type PolicySnapshotFunc func(ctx context.Context) (PolicySnapshot, error)

// ResolveSnapshot 解析当前策略快照。
func (f PolicySnapshotFunc) ResolveSnapshot(ctx context.Context) (PolicySnapshot, error) {
	if f == nil {
		return PolicySnapshot{}, errors.NewCode(
			errors.InvalidInput,
			"policy snapshot function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx)
}

// AuthzLogEntryType 区分审计日志的种类。
//
// 目前只有决策日志一种：约束写不单独留痕，写侧的「谁改了哪条」由
// `gochen/app/audited` 的业务审计覆盖，此处只记授权判定本身。
type AuthzLogEntryType string

const (
	// AuthzLogEntryTypeDecision 表示授权决策日志。
	AuthzLogEntryTypeDecision AuthzLogEntryType = "decision"
)

// AuthzLoggedResource 是审计日志中的资源快照。
type AuthzLoggedResource struct {
	Kind           string `json:"kind,omitempty"`
	ID             string `json:"id,omitempty"`
	ManagedScopeID int64  `json:"managed_scope_id,omitempty"`
	GlobalScope    bool   `json:"global_scope,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
	Revision       string `json:"revision,omitempty"`
}

// SnapshotVersionDerivedMetadataKey 标记快照版本是框架推导而非策略存储提供。
const SnapshotVersionDerivedMetadataKey = "snapshot_version_derived"

// AuthzLogEntry 是一条授权审计记录。
type AuthzLogEntry struct {
	ID                     string                `json:"id"`
	Type                   AuthzLogEntryType     `json:"type"`
	Timestamp              time.Time             `json:"timestamp"`
	DecisionID             string                `json:"decision_id,omitempty"`
	PrincipalID            string                `json:"principal_id,omitempty"`
	Action                 string                `json:"action,omitempty"`
	Operation              string                `json:"operation,omitempty"`
	Effect                 string                `json:"effect,omitempty"`
	ReasonCode             string                `json:"reason_code,omitempty"`
	SnapshotVersion        string                `json:"snapshot_version,omitempty"`
	SnapshotKey            string                `json:"snapshot_key,omitempty"`
	SnapshotVersionDerived bool                  `json:"snapshot_version_derived,omitempty"`
	Consistency            ConsistencyMode       `json:"consistency,omitempty"`
	LatencyMs              int64                 `json:"latency_ms,omitempty"`
	CacheHit               bool                  `json:"cache_hit,omitempty"`
	Resources              []AuthzLoggedResource `json:"resources,omitempty"`
	MatchedRules           []string              `json:"matched_rules,omitempty"`
	Execution              ExecutionMetadata     `json:"execution,omitempty"`
	Metadata               map[string]any        `json:"metadata,omitempty"`
}

// SanitizeAuthzLogEntry 返回可安全落库的审计记录副本。
//
// 只保留结构化授权事实，剔除空白与可能夹带敏感值的自由文本，
// 避免审计表成为二次泄露面（§5 安全与机密）。
func SanitizeAuthzLogEntry(entry AuthzLogEntry) AuthzLogEntry {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.DecisionID = strings.TrimSpace(entry.DecisionID)
	entry.PrincipalID = strings.TrimSpace(entry.PrincipalID)
	entry.Action = strings.TrimSpace(entry.Action)
	entry.Operation = strings.TrimSpace(entry.Operation)
	entry.Effect = strings.TrimSpace(entry.Effect)
	entry.ReasonCode = strings.TrimSpace(entry.ReasonCode)
	entry.SnapshotVersion = strings.TrimSpace(entry.SnapshotVersion)
	entry.SnapshotKey = strings.TrimSpace(entry.SnapshotKey)
	if entry.LatencyMs < 0 {
		entry.LatencyMs = 0
	}
	if len(entry.Resources) > 0 {
		resources := make([]AuthzLoggedResource, 0, len(entry.Resources))
		for _, resource := range entry.Resources {
			resource.Kind = strings.TrimSpace(resource.Kind)
			resource.ID = strings.TrimSpace(resource.ID)
			resource.TenantID = strings.TrimSpace(resource.TenantID)
			resource.OwnerID = strings.TrimSpace(resource.OwnerID)
			resource.Revision = strings.TrimSpace(resource.Revision)
			if resource.ManagedScopeID < 0 {
				resource.ManagedScopeID = 0
			}
			resources = append(resources, resource)
		}
		entry.Resources = resources
	}
	return entry
}

// IAuthzAuditStore 持久化授权审计记录。
//
// 经 Wrap 交付的条目已统一脱敏（SanitizeAuthzLogEntry + 默认资源脱敏策略），
// store 实现直接落库即可，无需重复处理；直接调用方自行构造条目时才需要
// 先过 SanitizeAuthzLogEntry。
type IAuthzAuditStore interface {
	SaveAuthzLogEntry(ctx context.Context, entry AuthzLogEntry) error
	ListAuthzLogEntries(ctx context.Context, entryType AuthzLogEntryType, limit int) ([]AuthzLogEntry, error)
}

// IAuthzMetrics 上报授权决策指标。
type IAuthzMetrics interface {
	RecordDecision(ctx context.Context, action string, effect string, latency time.Duration)
}

// AuthzMetricsFunc 允许用函数直接实现 IAuthzMetrics。
type AuthzMetricsFunc func(ctx context.Context, action string, effect string, latency time.Duration)

// RecordDecision 上报一次决策指标。
func (f AuthzMetricsFunc) RecordDecision(ctx context.Context, action string, effect string, latency time.Duration) {
	if f == nil {
		return
	}
	f(ctx, action, effect, latency)
}

// LoggedResourcesOf 把决策授权的资源投影为审计日志资源快照。
func LoggedResourcesOf(decision scoped.Decision) []AuthzLoggedResource {
	if len(decision.AuthorizedResources) == 0 {
		return nil
	}
	out := make([]AuthzLoggedResource, 0, len(decision.AuthorizedResources))
	for _, resource := range decision.AuthorizedResources {
		resource = resource.Normalize()
		out = append(out, AuthzLoggedResource{
			Kind:           resource.Kind,
			ID:             resource.ID,
			ManagedScopeID: resource.ManagedScopeID,
			GlobalScope:    resource.GlobalScope,
			TenantID:       resource.TenantID,
			OwnerID:        resource.OwnerID,
			Revision:       resource.Revision,
		})
	}
	return out
}

// NormalizeSnapshot 规范化快照的空白字段。
func NormalizeSnapshot(snapshot PolicySnapshot) PolicySnapshot {
	snapshot.Key = strings.TrimSpace(snapshot.Key)
	snapshot.Version = strings.TrimSpace(snapshot.Version)
	return snapshot
}

// IResourceLogSanitizer 对写入审计日志的资源做脱敏。
//
// 默认策略（见 DefaultSanitizeLoggedResource）会剔除 TenantID / OwnerID
// 这类可用于横向关联的标识，避免审计表成为二次泄露面。
// 需要保留完整字段的合规场景可注入自定义实现。
type IResourceLogSanitizer interface {
	SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource
}

// ResourceLogSanitizerFunc 允许用函数直接实现 IResourceLogSanitizer。
type ResourceLogSanitizerFunc func(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource

// SanitizeResourceForLog 对单个资源执行脱敏。
func (f ResourceLogSanitizerFunc) SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource {
	if f == nil {
		return DefaultSanitizeLoggedResource(resource)
	}
	return f(ctx, resource)
}

type resourceLogSanitizerContextKey struct{}

// WithResourceLogSanitizer 将资源脱敏器绑定到 context。
func WithResourceLogSanitizer(ctx context.Context, sanitizer IResourceLogSanitizer) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if sanitizer == nil {
		return ctx, nil
	}
	return context.WithValue(ctx, resourceLogSanitizerContextKey{}, sanitizer), nil
}

// ResourceLogSanitizerFromContext 从 context 读取资源脱敏器。
func ResourceLogSanitizerFromContext(ctx context.Context) (IResourceLogSanitizer, bool) {
	if ctx == nil {
		return nil, false
	}
	sanitizer, ok := ctx.Value(resourceLogSanitizerContextKey{}).(IResourceLogSanitizer)
	return sanitizer, ok && sanitizer != nil
}

// DefaultSanitizeLoggedResource 是默认脱敏策略：只保留授权判定必需的结构化事实。
func DefaultSanitizeLoggedResource(resource AuthzLoggedResource) AuthzLoggedResource {
	resource.TenantID = ""
	resource.OwnerID = ""
	return resource
}

// SanitizeLoggedResources 依据 ctx 中的脱敏器（或默认策略）处理资源列表。
func SanitizeLoggedResources(ctx context.Context, resources []AuthzLoggedResource) []AuthzLoggedResource {
	if len(resources) == 0 {
		return nil
	}
	sanitizer, ok := ResourceLogSanitizerFromContext(ctx)
	out := make([]AuthzLoggedResource, 0, len(resources))
	for _, resource := range resources {
		if ok {
			out = append(out, sanitizer.SanitizeResourceForLog(ctx, resource))
			continue
		}
		out = append(out, DefaultSanitizeLoggedResource(resource))
	}
	return out
}
