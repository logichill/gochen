package auth

import (
	"context"
	"strings"
)

type snapshotResolverContextKey struct{}

// ISnapshotResolver 定义策略快照解析器。
type ISnapshotResolver interface {
	ResolveSnapshot(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error)
}

// SnapshotResolverFunc 允许用函数直接实现快照解析器。
type SnapshotResolverFunc func(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error)

// ResolveSnapshot 解析当前授权评估所使用的策略快照。
func (f SnapshotResolverFunc) ResolveSnapshot(ctx context.Context, eval AuthzEvalContext) (SnapshotResolution, error) {
	return f(ctx, eval)
}

// WithSnapshotResolver 将快照解析器绑定到 context。
func WithSnapshotResolver(ctx context.Context, resolver ISnapshotResolver) (context.Context, error) {
	ctx, err := RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		return ctx, nil
	}
	return context.WithValue(ctx, snapshotResolverContextKey{}, resolver), nil
}

// SnapshotResolverFromContext 从 context 读取快照解析器。
func SnapshotResolverFromContext(ctx context.Context) (ISnapshotResolver, bool) {
	if ctx == nil {
		return nil, false
	}
	resolver, ok := ctx.Value(snapshotResolverContextKey{}).(ISnapshotResolver)
	return resolver, ok && resolver != nil
}

func finalizeEvalContext(ctx context.Context, eval AuthzEvalContext) (AuthzEvalContext, error) {
	eval = normalizeEvalContext(eval)
	if strings.TrimSpace(eval.SnapshotVersion) != "" {
		return eval, nil
	}
	if resolver, ok := SnapshotResolverFromContext(ctx); ok {
		resolution, err := resolver.ResolveSnapshot(ctx, eval)
		if err != nil {
			return AuthzEvalContext{}, err
		}
		if strings.TrimSpace(resolution.Version) != "" {
			eval.SnapshotVersion = strings.TrimSpace(resolution.Version)
			eval.SnapshotKey = strings.TrimSpace(resolution.Key)
			eval.SnapshotCacheHit = resolution.CacheHit
			eval.SnapshotVersionDerived = false
			return normalizeEvalContext(eval), nil
		}
	}
	eval.SnapshotVersion = deriveSnapshotVersion(eval.Consistency, eval.Execution)
	eval.SnapshotVersionDerived = true
	return normalizeEvalContext(eval), nil
}

// BindAuthzEvalContext 解析并回写当前授权评估上下文，确保后续链路共享同一 snapshot/version。
func BindAuthzEvalContext(ctx context.Context) (context.Context, AuthzEvalContext, error) {
	eval, err := EvalContextFromContext(ctx)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	bound, err := WithAuthzEvalContext(ctx, eval)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	return bound, eval, nil
}

// BindAuthzEvalContextOrEmpty 解析并回写当前授权评估上下文；主体缺失时保留 best-effort 运行时信息。
func BindAuthzEvalContextOrEmpty(ctx context.Context) (context.Context, AuthzEvalContext, error) {
	eval, err := EvalContextFromContextOrEmpty(ctx)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	bound, err := WithAuthzEvalContext(ctx, eval)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	return bound, eval, nil
}

// PrepareReplayAuthorization 为异步 replay/repair/job worker 构造一次新的重授权运行时。
//
// 该 helper 会清空历史 decision/snapshot 痕迹，并强制切回 strong consistency，
// 避免后台流程偷用旧 allow 结果。
func PrepareReplayAuthorization(ctx context.Context, metadata ExecutionMetadata) (context.Context, AuthzEvalContext, error) {
	baseEval, err := EvalContextFromContextOrEmpty(ctx)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	metadata = mergeReplayExecutionMetadata(baseEval.Execution, metadata)

	replayEval := baseEval
	replayEval.Execution = metadata
	replayEval.Consistency = ConsistencyModeStrong
	replayEval.SnapshotVersion = ""
	replayEval.SnapshotVersionDerived = false
	replayEval.SnapshotCacheHit = false

	ctx, err = WithExecutionMetadata(ctx, metadata)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	ctx, err = WithConsistencyMode(ctx, ConsistencyModeStrong)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	replayEval, err = finalizeEvalContext(ctx, replayEval)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	ctx, err = WithAuthzEvalContext(ctx, replayEval)
	if err != nil {
		return nil, AuthzEvalContext{}, err
	}
	return ctx, replayEval, nil
}

func mergeReplayExecutionMetadata(base, override ExecutionMetadata) ExecutionMetadata {
	base = normalizeExecutionMetadata(base)
	override = normalizeExecutionMetadata(override)
	if carriesNewExecutionAnchor(override) {
		base.RequestID = ""
		base.EventID = ""
		base.JobID = ""
	}
	merged := mergeExecutionMetadata(base, override)
	merged.DecisionID = ""
	return normalizeExecutionMetadata(merged)
}

func carriesNewExecutionAnchor(metadata ExecutionMetadata) bool {
	metadata = normalizeExecutionMetadata(metadata)
	return metadata.RequestID != "" || metadata.EventID != "" || metadata.JobID != ""
}
