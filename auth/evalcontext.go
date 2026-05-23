package auth

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"strconv"
	"strings"
)

type authzEvalContextKey struct{}
type consistencyModeContextKey struct{}
type executionMetadataContextKey struct{}
type highRiskAuthzContextKey struct{}

// ConsistencyMode 表示授权评估与写入保护所要求的一致性级别。
type ConsistencyMode string

const (
	// ConsistencyModeUnspecified 表示未显式声明，由框架按默认值处理。
	ConsistencyModeUnspecified ConsistencyMode = ""
	// ConsistencyModeStrong 表示必须基于当前时刻的实时权限事实进行判定。
	ConsistencyModeStrong ConsistencyMode = "strong"
	// ConsistencyModeBoundedStaleness 表示允许在受控范围内使用陈旧快照。
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

// AuthzEvalContext 表达一次授权求值共享的请求内上下文。
type AuthzEvalContext struct {
	Principal              Principal
	SnapshotVersion        string
	SnapshotKey            string
	SnapshotCacheHit       bool
	SnapshotVersionDerived bool
	Consistency            ConsistencyMode
	Execution              ExecutionMetadata
}

// WithConsistencyMode 将一致性级别写入 context。
func WithConsistencyMode(ctx context.Context, mode ConsistencyMode) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	mode = normalizeConsistencyMode(mode)
	if mode == ConsistencyModeUnspecified {
		return ctx, nil
	}
	return context.WithValue(ctx, consistencyModeContextKey{}, mode), nil
}

// ConsistencyModeFromContext 从 context 读取一致性级别。
func ConsistencyModeFromContext(ctx context.Context) (ConsistencyMode, bool) {
	if ctx == nil {
		return ConsistencyModeUnspecified, false
	}
	mode, ok := ctx.Value(consistencyModeContextKey{}).(ConsistencyMode)
	if !ok {
		return ConsistencyModeUnspecified, false
	}
	mode = normalizeConsistencyMode(mode)
	return mode, mode != ConsistencyModeUnspecified
}

// WithExecutionMetadata 将执行元数据写入 context。
func WithExecutionMetadata(ctx context.Context, metadata ExecutionMetadata) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, executionMetadataContextKey{}, normalizeExecutionMetadata(metadata)), nil
}

// ExecutionMetadataFromContext 从 context 读取执行元数据。
func ExecutionMetadataFromContext(ctx context.Context) (ExecutionMetadata, bool) {
	if ctx == nil {
		return ExecutionMetadata{}, false
	}
	metadata, ok := ctx.Value(executionMetadataContextKey{}).(ExecutionMetadata)
	if !ok {
		return ExecutionMetadata{}, false
	}
	metadata = normalizeExecutionMetadata(metadata)
	if metadata == (ExecutionMetadata{}) {
		return ExecutionMetadata{}, false
	}
	return metadata, true
}

// WithSnapshotVersion 将快照版本写入 context。
func WithSnapshotVersion(ctx context.Context, snapshotVersion string) (context.Context, error) {
	eval, err := EvalContextFromContextOrEmpty(ctx)
	if err != nil {
		return nil, err
	}
	eval.SnapshotVersion = strings.TrimSpace(snapshotVersion)
	return WithAuthzEvalContext(ctx, eval)
}

// SnapshotVersionFromContext 从 context 读取快照版本。
func SnapshotVersionFromContext(ctx context.Context) (string, bool) {
	eval, ok := AuthzEvalContextFromContext(ctx)
	if ok && strings.TrimSpace(eval.SnapshotVersion) != "" {
		return strings.TrimSpace(eval.SnapshotVersion), true
	}
	return "", false
}

// WithAuthzEvalContext 将完整授权求值上下文写入 context。
func WithAuthzEvalContext(ctx context.Context, eval AuthzEvalContext) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	eval = normalizeEvalContext(eval)
	tenantID := strings.TrimSpace(TenantID(ctx))
	if !isZeroPrincipal(eval.Principal) {
		ctx, err = WithPrincipal(ctx, eval.Principal)
		if err != nil {
			return nil, err
		}
	}
	if tenantID != "" {
		ctx, err = WithTenantID(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	if eval.Consistency != ConsistencyModeUnspecified {
		ctx = context.WithValue(ctx, consistencyModeContextKey{}, eval.Consistency)
	}
	if eval.Execution != (ExecutionMetadata{}) {
		ctx = context.WithValue(ctx, executionMetadataContextKey{}, eval.Execution)
	}
	return context.WithValue(ctx, authzEvalContextKey{}, eval), nil
}

// AuthzEvalContextFromContext 读取显式绑定的授权求值上下文。
func AuthzEvalContextFromContext(ctx context.Context) (AuthzEvalContext, bool) {
	if ctx == nil {
		return AuthzEvalContext{}, false
	}
	eval, ok := ctx.Value(authzEvalContextKey{}).(AuthzEvalContext)
	if !ok {
		return AuthzEvalContext{}, false
	}
	eval = normalizeEvalContext(eval)
	if isZeroEvalContext(eval) {
		return AuthzEvalContext{}, false
	}
	return eval, true
}

// WithHighRiskAuthorization 标记当前授权/写路径为高风险路径。
func WithHighRiskAuthorization(ctx context.Context) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, highRiskAuthzContextKey{}, true), nil
}

// IsHighRiskAuthorizationFromContext 判断当前 context 是否被标记为高风险授权路径。
func IsHighRiskAuthorizationFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, _ := ctx.Value(highRiskAuthzContextKey{}).(bool)
	return flag
}

// EvalContextFromContext 从 context 中构造阶段四授权上下文。
func EvalContextFromContext(ctx context.Context) (AuthzEvalContext, error) {
	if eval, ok := AuthzEvalContextFromContext(ctx); ok {
		eval, err := mergeEvalContext(ctx, eval, true)
		if err != nil {
			return AuthzEvalContext{}, err
		}
		return finalizeEvalContext(ctx, eval)
	}
	eval, err := buildEvalContextFromContext(ctx)
	if err != nil {
		return AuthzEvalContext{}, err
	}
	return finalizeEvalContext(ctx, eval)
}

// EvalContextFromContextOrEmpty 从 context 中读取求值上下文；若 principal 缺失则返回空上下文。
func EvalContextFromContextOrEmpty(ctx context.Context) (AuthzEvalContext, error) {
	if eval, ok := AuthzEvalContextFromContext(ctx); ok {
		eval, err := mergeEvalContext(ctx, eval, false)
		if err != nil {
			return AuthzEvalContext{}, err
		}
		return finalizeEvalContext(ctx, eval)
	}
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return finalizeEvalContext(ctx, buildBestEffortEvalContext(ctx))
	}
	eval := buildBestEffortEvalContext(ctx)
	eval.Principal = principal
	return finalizeEvalContext(ctx, eval)
}

func buildEvalContextFromContext(ctx context.Context) (AuthzEvalContext, error) {
	principal, err := RequirePrincipal(ctx)
	if err != nil {
		return AuthzEvalContext{}, err
	}
	eval := buildBestEffortEvalContext(ctx)
	eval.Principal = principal
	return normalizeEvalContext(eval), nil
}

func buildBestEffortEvalContext(ctx context.Context) AuthzEvalContext {
	eval := AuthzEvalContext{
		Consistency: ConsistencyModeStrong,
		Execution:   inferExecutionMetadata(ctx),
	}
	if mode, ok := ConsistencyModeFromContext(ctx); ok {
		eval.Consistency = mode
	}
	if snapshotVersion, ok := SnapshotVersionFromContext(ctx); ok {
		eval.SnapshotVersion = snapshotVersion
	}
	return normalizeEvalContext(eval)
}

func mergeEvalContext(ctx context.Context, eval AuthzEvalContext, requirePrincipal bool) (AuthzEvalContext, error) {
	merged := buildBestEffortEvalContext(ctx)
	if !isZeroPrincipal(eval.Principal) {
		merged.Principal = eval.Principal
	} else if principal, ok := PrincipalFromContext(ctx); ok {
		merged.Principal = principal
	}
	if eval.SnapshotVersion != "" {
		merged.SnapshotVersion = eval.SnapshotVersion
	}
	if eval.SnapshotKey != "" {
		merged.SnapshotKey = eval.SnapshotKey
	}
	if eval.SnapshotCacheHit {
		merged.SnapshotCacheHit = true
	}
	if eval.SnapshotVersionDerived {
		merged.SnapshotVersionDerived = true
	}
	if eval.Consistency != ConsistencyModeUnspecified {
		merged.Consistency = eval.Consistency
	}
	merged.Execution = mergeExecutionMetadata(merged.Execution, eval.Execution)
	if requirePrincipal && isZeroPrincipal(merged.Principal) {
		return AuthzEvalContext{}, errors.NewCode(errors.Unauthorized, "principal is required")
	}
	return normalizeEvalContext(merged), nil
}

func inferExecutionMetadata(ctx context.Context) ExecutionMetadata {
	metadata := ExecutionMetadata{}
	if bound, ok := ExecutionMetadataFromContext(ctx); ok {
		metadata = mergeExecutionMetadata(metadata, bound)
	}
	if requestID := strings.TrimSpace(contextx.RequestID(ctx)); requestID != "" && metadata.RequestID == "" {
		metadata.RequestID = requestID
	}
	if principal, ok := PrincipalFromContext(ctx); ok {
		if metadata.ActorID == "" && principal.SubjectID > 0 {
			metadata.ActorID = strconv.FormatInt(principal.SubjectID, 10)
		}
		if metadata.InitiatorID == "" && metadata.ActorID != "" {
			metadata.InitiatorID = metadata.ActorID
		}
	}
	return normalizeExecutionMetadata(metadata)
}

func mergeExecutionMetadata(base, override ExecutionMetadata) ExecutionMetadata {
	base = normalizeExecutionMetadata(base)
	override = normalizeExecutionMetadata(override)
	if override.InitiatorID != "" {
		base.InitiatorID = override.InitiatorID
	}
	if override.ActorID != "" {
		base.ActorID = override.ActorID
	}
	if override.RequestID != "" {
		base.RequestID = override.RequestID
	}
	if override.DecisionID != "" {
		base.DecisionID = override.DecisionID
	}
	if override.EventID != "" {
		base.EventID = override.EventID
	}
	if override.JobID != "" {
		base.JobID = override.JobID
	}
	return normalizeExecutionMetadata(base)
}

func deriveSnapshotVersion(mode ConsistencyMode, execution ExecutionMetadata) string {
	mode = normalizeConsistencyMode(mode)
	switch {
	case execution.RequestID != "":
		return "req:" + execution.RequestID
	case execution.JobID != "":
		return "job:" + execution.JobID
	case execution.EventID != "":
		return "event:" + execution.EventID
	case mode == ConsistencyModeBoundedStaleness:
		return "bounded"
	default:
		return "live"
	}
}

func normalizeExecutionMetadata(metadata ExecutionMetadata) ExecutionMetadata {
	metadata.InitiatorID = strings.TrimSpace(metadata.InitiatorID)
	metadata.ActorID = strings.TrimSpace(metadata.ActorID)
	metadata.RequestID = strings.TrimSpace(metadata.RequestID)
	metadata.DecisionID = strings.TrimSpace(metadata.DecisionID)
	metadata.EventID = strings.TrimSpace(metadata.EventID)
	metadata.JobID = strings.TrimSpace(metadata.JobID)
	return metadata
}

func normalizeConsistencyMode(mode ConsistencyMode) ConsistencyMode {
	switch ConsistencyMode(strings.TrimSpace(string(mode))) {
	case ConsistencyModeStrong:
		return ConsistencyModeStrong
	case ConsistencyModeBoundedStaleness:
		return ConsistencyModeBoundedStaleness
	default:
		return ConsistencyModeUnspecified
	}
}

func normalizeEvalContext(eval AuthzEvalContext) AuthzEvalContext {
	eval.Principal = eval.Principal.Clone()
	eval.SnapshotVersion = strings.TrimSpace(eval.SnapshotVersion)
	eval.SnapshotKey = strings.TrimSpace(eval.SnapshotKey)
	eval.Consistency = normalizeConsistencyMode(eval.Consistency)
	if eval.Consistency == ConsistencyModeUnspecified {
		eval.Consistency = ConsistencyModeStrong
	}
	eval.Execution = normalizeExecutionMetadata(eval.Execution)
	if eval.Execution.ActorID == "" && eval.Principal.SubjectID > 0 {
		eval.Execution.ActorID = strconv.FormatInt(eval.Principal.SubjectID, 10)
	}
	if eval.Execution.InitiatorID == "" {
		eval.Execution.InitiatorID = eval.Execution.ActorID
	}
	return eval
}

func isZeroPrincipal(principal Principal) bool {
	principal = principal.Clone()
	return principal.SubjectID == 0 &&
		principal.HomeTenantID == 0 &&
		principal.HomeScopeID == 0 &&
		principal.ActiveScopeID == 0 &&
		principal.ActiveBindingID == 0 &&
		len(principal.Roles) == 0 &&
		len(principal.Permissions) == 0 &&
		len(principal.PermissionSet) == 0 &&
		!principal.IsSystem
}

func isZeroEvalContext(eval AuthzEvalContext) bool {
	return isZeroPrincipal(eval.Principal) &&
		strings.TrimSpace(eval.SnapshotVersion) == "" &&
		strings.TrimSpace(eval.SnapshotKey) == "" &&
		!eval.SnapshotCacheHit &&
		!eval.SnapshotVersionDerived &&
		normalizeConsistencyMode(eval.Consistency) == ConsistencyModeUnspecified &&
		eval.Execution == (ExecutionMetadata{})
}
