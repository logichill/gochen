package auth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"gochen/errors"
	"gochen/ident"
	"gochen/logging"
	"gochen/observe"
)

type authzLogStoreContextKey struct{}
type authzMetricsContextKey struct{}
type resourceLogSanitizerContextKey struct{}

// AuthzLogEntryType 表示 authz 运行时事件类型。
type AuthzLogEntryType string

const (
	// AuthzLogEntryTypeDecision 表示授权决策日志。
	AuthzLogEntryTypeDecision AuthzLogEntryType = "decision"
	// AuthzLogEntryTypeWrite 表示写入审计日志。
	AuthzLogEntryTypeWrite AuthzLogEntryType = "write"
)

// AuthzLoggedResource 表示进入 authz log 的已脱敏资源快照。
type AuthzLoggedResource struct {
	Kind           string `json:"kind"`
	ID             string `json:"id,omitempty"`
	ManagedScopeID int64  `json:"managed_scope_id,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
	Revision       string `json:"revision,omitempty"`
}

// AuthzLogEntry 表示一条授权决策或写入审计记录。
type AuthzLogEntry struct {
	ID                     string                `json:"id"`
	Type                   AuthzLogEntryType     `json:"type"`
	Timestamp              time.Time             `json:"timestamp"`
	DecisionID             string                `json:"decision_id,omitempty"`
	PrincipalID            string                `json:"principal_id,omitempty"`
	Permission             string                `json:"permission,omitempty"`
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

// IAuthzLogStore 定义 authz 决策/写入日志存储接口。
type IAuthzLogStore interface {
	SaveAuthzLogEntry(ctx context.Context, entry AuthzLogEntry) error
	ListAuthzLogEntries(ctx context.Context, entryType AuthzLogEntryType, limit int) ([]AuthzLogEntry, error)
}

// IResourceLogSanitizer 定义资源日志脱敏接口。
type IResourceLogSanitizer interface {
	SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource
}

// ResourceLogSanitizerFunc 允许用函数直接实现资源日志脱敏。
type ResourceLogSanitizerFunc func(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource

// SanitizeResourceForLog 执行资源日志脱敏。
func (f ResourceLogSanitizerFunc) SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource {
	return f(ctx, resource)
}

// WithAuthzLogStore 把授权日志存储挂到 context，并顺带补齐写审计 recorder。
//
// 这样无论是显式授权决策日志，还是 WriteGuard 触发的写审计，都能沿用同一份
// 运行时观测配置。
func WithAuthzLogStore(ctx context.Context, store IAuthzLogStore) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return ctx, nil
	}
	ctx = context.WithValue(ctx, authzLogStoreContextKey{}, store)
	return withDefaultAccessWriteAuditRecorder(ctx), nil
}

func AuthzLogStoreFromContext(ctx context.Context) (IAuthzLogStore, bool) {
	if ctx == nil {
		return nil, false
	}
	store, ok := ctx.Value(authzLogStoreContextKey{}).(IAuthzLogStore)
	return store, ok && store != nil
}

// WithAuthzMetrics 把授权指标收集器挂到 context，并确保写审计也能打点。
func WithAuthzMetrics(ctx context.Context, metrics observe.IMetrics) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if metrics == nil {
		return ctx, nil
	}
	ctx = context.WithValue(ctx, authzMetricsContextKey{}, metrics)
	return withDefaultAccessWriteAuditRecorder(ctx), nil
}

func AuthzMetricsFromContext(ctx context.Context) (observe.IMetrics, bool) {
	if ctx == nil {
		return nil, false
	}
	metrics, ok := ctx.Value(authzMetricsContextKey{}).(observe.IMetrics)
	return metrics, ok && metrics != nil
}

// WithResourceLogSanitizer 绑定资源日志脱敏器，用于在落日志前裁剪敏感字段。
func WithResourceLogSanitizer(ctx context.Context, sanitizer IResourceLogSanitizer) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if sanitizer == nil {
		return ctx, nil
	}
	return context.WithValue(ctx, resourceLogSanitizerContextKey{}, sanitizer), nil
}

func resourceLogSanitizerFromContext(ctx context.Context) (IResourceLogSanitizer, bool) {
	if ctx == nil {
		return nil, false
	}
	sanitizer, ok := ctx.Value(resourceLogSanitizerContextKey{}).(IResourceLogSanitizer)
	return sanitizer, ok && sanitizer != nil
}

// MemoryAuthzLogStore 是一个仅保存在进程内存中的授权日志存储，适合测试或本地调试。
type MemoryAuthzLogStore struct {
	mu      sync.RWMutex
	entries []AuthzLogEntry
}

// NewMemoryAuthzLogStore 创建一个按追加顺序保存日志的内存实现。
func NewMemoryAuthzLogStore() *MemoryAuthzLogStore {
	return &MemoryAuthzLogStore{
		entries: make([]AuthzLogEntry, 0, 32),
	}
}

// SaveAuthzLogEntry 规范化日志内容后追加保存到内存切片。
func (s *MemoryAuthzLogStore) SaveAuthzLogEntry(ctx context.Context, entry AuthzLogEntry) error {
	_ = ctx
	entry = normalizeAuthzLogEntry(entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, cloneAuthzLogEntry(entry))
	return nil
}

// ListAuthzLogEntries 按时间倒序返回日志，可按类型过滤并限制返回条数。
func (s *MemoryAuthzLogStore) ListAuthzLogEntries(ctx context.Context, entryType AuthzLogEntryType, limit int) ([]AuthzLogEntry, error) {
	_ = ctx
	entryType = normalizeAuthzLogEntryType(entryType)
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]AuthzLogEntry, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		if entryType != "" && entry.Type != entryType {
			continue
		}
		result = append(result, cloneAuthzLogEntry(entry))
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// RecordWriteAudit 把一次 WriteGuard 写入动作落成审计日志，并同步上报指标。
func RecordWriteAudit(ctx context.Context, operation string, guard WriteGuard, resources ...ResourceWriteGuard) {
	store, ok := AuthzLogStoreFromContext(ctx)
	if !ok {
		recordAuthzWriteMetrics(ctx, operation, false)
		return
	}
	eval, _ := EvalContextFromContextOrEmpty(ctx)
	entry := AuthzLogEntry{
		Type:                   AuthzLogEntryTypeWrite,
		DecisionID:             guard.DecisionID,
		PrincipalID:            principalIDForLog(eval.Principal),
		Operation:              strings.TrimSpace(operation),
		SnapshotVersion:        guard.SnapshotVersion,
		SnapshotKey:            strings.TrimSpace(eval.SnapshotKey),
		SnapshotVersionDerived: eval.SnapshotVersionDerived,
		Consistency:            normalizeConsistencyMode(guard.Consistency),
		CacheHit:               eval.SnapshotCacheHit,
		Resources:              logResourcesFromWriteGuards(ctx, resources),
		Execution:              eval.Execution,
		Metadata: map[string]any{
			"resource_count": len(resources),
		},
	}
	if err := store.SaveAuthzLogEntry(ctx, entry); err != nil {
		authzLogger().Warn(ctx, "failed to persist authz write log", logging.Error(err))
		recordAuthzWriteMetrics(ctx, operation, true)
		return
	}
	recordAuthzWriteMetrics(ctx, operation, false)
}

func recordAuthorizationOutcome(
	ctx context.Context,
	eval AuthzEvalContext,
	permission string,
	resources []Resource,
	decision AuthzDecision,
	latency time.Duration,
	authErr error,
) {
	recordAuthzDecisionMetrics(ctx, permission, eval, decision, latency, authErr)

	store, ok := AuthzLogStoreFromContext(ctx)
	if !ok {
		return
	}

	entry := AuthzLogEntry{
		Type:                   AuthzLogEntryTypeDecision,
		DecisionID:             decision.ID,
		PrincipalID:            principalIDForLog(eval.Principal),
		Permission:             strings.TrimSpace(permission),
		Effect:                 strings.TrimSpace(string(decision.Effect)),
		ReasonCode:             decision.ReasonCode,
		SnapshotVersion:        decision.SnapshotVersion,
		SnapshotKey:            strings.TrimSpace(eval.SnapshotKey),
		SnapshotVersionDerived: eval.SnapshotVersionDerived,
		Consistency:            decision.Consistency,
		LatencyMs:              latency.Milliseconds(),
		CacheHit:               eval.SnapshotCacheHit,
		Resources:              logResourcesFromResources(ctx, resources),
		MatchedRules:           append([]string(nil), decision.MatchedRules...),
		Execution:              eval.Execution,
	}
	if authErr != nil {
		entry.Metadata = map[string]any{
			"error_code": string(errors.Code(authErr)),
			"error":      authErr.Error(),
		}
	}
	if err := store.SaveAuthzLogEntry(ctx, entry); err != nil {
		authzLogger().Warn(ctx, "failed to persist authz decision log", logging.Error(err))
	}
}

func recordAuthzDecisionMetrics(
	ctx context.Context,
	permission string,
	eval AuthzEvalContext,
	decision AuthzDecision,
	latency time.Duration,
	authErr error,
) {
	metrics, ok := AuthzMetricsFromContext(ctx)
	if !ok {
		return
	}
	effect := strings.TrimSpace(string(decision.Effect))
	if authErr != nil {
		effect = "error"
	}
	labels := map[string]string{
		"permission": strings.TrimSpace(permission),
		"effect":     effect,
		"cache_hit":  boolLabel(eval.SnapshotCacheHit),
	}
	metrics.Counter("authz_decision_total", 1, labels)
	metrics.Histogram("authz_decision_latency_ms", float64(latency.Milliseconds()), labels)
}

func recordAuthzWriteMetrics(ctx context.Context, operation string, hasErr bool) {
	metrics, ok := AuthzMetricsFromContext(ctx)
	if !ok {
		return
	}
	labels := map[string]string{
		"operation": strings.TrimSpace(operation),
		"error":     boolLabel(hasErr),
	}
	metrics.Counter("authz_write_total", 1, labels)
}

func logResourcesFromResources(ctx context.Context, resources []Resource) []AuthzLoggedResource {
	if len(resources) == 0 {
		return nil
	}
	out := make([]AuthzLoggedResource, 0, len(resources))
	for _, resource := range resources {
		out = append(out, sanitizeResourceForLog(ctx, AuthzLoggedResource{
			Kind:           resource.Kind,
			ID:             resource.ID,
			ManagedScopeID: resource.ManagedScopeID,
			OwnerID:        resource.OwnerID,
			Revision:       resource.Revision,
		}))
	}
	return out
}

func logResourcesFromWriteGuards(ctx context.Context, resources []ResourceWriteGuard) []AuthzLoggedResource {
	if len(resources) == 0 {
		return nil
	}
	out := make([]AuthzLoggedResource, 0, len(resources))
	for _, resource := range resources {
		out = append(out, sanitizeResourceForLog(ctx, AuthzLoggedResource{
			Kind:           resource.Kind,
			ID:             resource.ResourceID,
			ManagedScopeID: resource.ManagedScopeID,
			Revision:       resource.Revision,
		}))
	}
	return out
}

func sanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource {
	resource = normalizeLoggedResource(resource)
	if sanitizer, ok := resourceLogSanitizerFromContext(ctx); ok {
		return normalizeLoggedResource(sanitizer.SanitizeResourceForLog(ctx, resource))
	}
	return resource
}

func normalizeAuthzLogEntry(entry AuthzLogEntry) AuthzLogEntry {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.Type = normalizeAuthzLogEntryType(entry.Type)
	if entry.Type == "" {
		entry.Type = AuthzLogEntryTypeDecision
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.ID == "" {
		entry.ID = nextAuthzLogID()
	}
	entry.DecisionID = strings.TrimSpace(entry.DecisionID)
	entry.PrincipalID = strings.TrimSpace(entry.PrincipalID)
	entry.Permission = strings.TrimSpace(entry.Permission)
	entry.Operation = strings.TrimSpace(entry.Operation)
	entry.Effect = strings.TrimSpace(entry.Effect)
	entry.ReasonCode = strings.TrimSpace(entry.ReasonCode)
	entry.SnapshotVersion = strings.TrimSpace(entry.SnapshotVersion)
	entry.SnapshotKey = strings.TrimSpace(entry.SnapshotKey)
	entry.Consistency = normalizeConsistencyMode(entry.Consistency)
	entry.MatchedRules = normalizeStrings(entry.MatchedRules)
	entry.Execution = normalizeExecutionMetadata(entry.Execution)
	if len(entry.Resources) > 0 {
		resources := make([]AuthzLoggedResource, 0, len(entry.Resources))
		for _, resource := range entry.Resources {
			resources = append(resources, normalizeLoggedResource(resource))
		}
		entry.Resources = resources
	} else {
		entry.Resources = nil
	}
	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}
	return entry
}

func normalizeLoggedResource(resource AuthzLoggedResource) AuthzLoggedResource {
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.ManagedScopeID = NormalizePositiveID(resource.ManagedScopeID)
	resource.OwnerID = strings.TrimSpace(resource.OwnerID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	return resource
}

func normalizeAuthzLogEntryType(entryType AuthzLogEntryType) AuthzLogEntryType {
	switch AuthzLogEntryType(strings.TrimSpace(string(entryType))) {
	case AuthzLogEntryTypeDecision:
		return AuthzLogEntryTypeDecision
	case AuthzLogEntryTypeWrite:
		return AuthzLogEntryTypeWrite
	default:
		return ""
	}
}

func cloneAuthzLogEntry(entry AuthzLogEntry) AuthzLogEntry {
	entry.Resources = cloneAuthzLoggedResources(entry.Resources)
	entry.MatchedRules = append([]string(nil), entry.MatchedRules...)
	entry.Metadata = cloneAuthzMetadata(entry.Metadata)
	return entry
}

func cloneAuthzLoggedResources(resources []AuthzLoggedResource) []AuthzLoggedResource {
	if len(resources) == 0 {
		return nil
	}
	return append([]AuthzLoggedResource(nil), resources...)
}

func cloneAuthzMetadata(metadata map[string]any) map[string]any {
	return cloneMetadataMap(metadata)
}

func authzLogEntryResourcesJSON(entry AuthzLogEntry) string {
	if len(entry.Resources) == 0 {
		return "[]"
	}
	b, err := json.Marshal(entry.Resources)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func authzLogEntryExecutionJSON(entry AuthzLogEntry) string {
	b, err := json.Marshal(entry.Execution)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// snapshotVersionDerivedMetadataKey 是 SnapshotVersionDerived 落到 metadata_json
// 时使用的保留 key。读取端会在 decode 后剥离此 key，避免暴露给应用层 Metadata。
const snapshotVersionDerivedMetadataKey = "__snapshot_version_derived__"

func authzLogEntryMetadataJSON(entry AuthzLogEntry) string {
	if len(entry.Metadata) == 0 && len(entry.MatchedRules) == 0 && !entry.SnapshotVersionDerived {
		return "{}"
	}
	payload := map[string]any{}
	for k, v := range entry.Metadata {
		payload[k] = v
	}
	if len(entry.MatchedRules) > 0 {
		payload["matched_rules"] = append([]string(nil), entry.MatchedRules...)
	}
	if entry.SnapshotVersionDerived {
		payload[snapshotVersionDerivedMetadataKey] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func principalIDForLog(principal Principal) string {
	principal = principal.Clone()
	if principal.SubjectID > 0 {
		return strconv.FormatInt(principal.SubjectID, 10)
	}
	if principal.IsSystem {
		return "system"
	}
	return ""
}

func nextAuthzLogID() string {
	generator := ident.DefaultStringGenerator()
	if generator == nil {
		return ""
	}
	id, err := generator.Next()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func authzLogger() logging.ILogger {
	return logging.ComponentLogger("auth.runtime")
}
