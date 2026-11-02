package governance

import (
	"context"
	"strings"
	"time"

	"gochen/auth"
)

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

// LoggedResourcesOf 记录请求明确列出的资源标识。
func LoggedResourcesOf(req auth.Request) []AuthzLoggedResource {
	out := make([]AuthzLoggedResource, 0, len(req.IDs))
	for _, id := range req.IDs {
		out = append(out, AuthzLoggedResource{Kind: req.Resource, ID: id})
	}
	return out
}
