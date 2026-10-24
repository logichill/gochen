package scoped

import (
	"context"
	"fmt"
	"strings"

	"gochen/auth/action"
	authscoped "gochen/auth/scoped"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/errors"
)

// requireScopeDeclaration 断言底层仓储确实会消费写约束，且真的执行得了约束写。
func requireScopeDeclaration[T domain.IEntity[ID], ID comparable](
	repo domcrud.IRepository[T, ID],
	entityType string,
) error {
	probe, ok := any(repo).(authscoped.IScopeDeclarationProbe)
	if !ok {
		return errors.NewCode(errors.Unsupported,
			"L3 scoped application requires a repository that consumes write constraints; "+
				"repository does not implement scoped.IScopeDeclarationProbe").
			WithContext("entity_type", entityType).
			WithContext("repository_type", fmt.Sprintf("%T", repo))
	}
	if !probe.HasScopeDeclaration() {
		return errors.NewCode(errors.InvalidInput,
			"L3 scoped application requires the repository to declare scope columns (e.g. ormrepo.WithScope); "+
				"otherwise write constraints would be silently dropped").
			WithContext("entity_type", entityType).
			WithContext("repository_type", fmt.Sprintf("%T", repo))
	}
	// 约束按实体类型定向投放，装饰器与仓储必须用同一个标识，否则恒匹配不到。
	kindProbe, ok := any(repo).(authscoped.IResourceKindProbe)
	if !ok {
		return errors.NewCode(errors.Unsupported,
			"L3 scoped application requires a repository that exposes its resource kind; "+
				"repository does not implement scoped.IResourceKindProbe")
	}
	repoKind := strings.ToLower(strings.TrimSpace(kindProbe.ResourceKind()))
	if repoKind != strings.ToLower(strings.TrimSpace(entityType)) {
		return errors.NewCode(errors.InvalidInput,
			"L3 entity type does not match the repository resource kind; "+
				"write constraints would never match (align Config.EntityType with the repository resource kind, "+
				"e.g. ormrepo.WithResourceKind)").
			WithContext("entity_type", entityType).
			WithContext("repository_resource_kind", kindProbe.ResourceKind())
	}
	// 能消费约束还不够，还得能验证约束是否命中：驱动不支持 RowsAffected 时
	// 约束写会退化为盲写。把这一预检放在装配期，而不是等第一条写请求。
	writeProbe, ok := any(repo).(authscoped.IConstraintWriteProbe)
	if !ok {
		return errors.NewCode(errors.Unsupported,
			"L3 scoped application requires a repository that validates constrained writes; "+
				"repository does not implement scoped.IConstraintWriteProbe")
	}
	if err := writeProbe.ValidateWriteConstraintSupport(); err != nil {
		return err
	}
	return nil
}

// authorizeWrite 完成 PDP 判定并返回携带约束与数据范围的一次性派生 ctx。
func (a *application[T, ID]) authorizeWrite(ctx context.Context, op action.Operation, target any) (context.Context, error) {
	code, ok := a.config.Policy.CodeFor(op)
	if !ok {
		return nil, errors.NewCode(errors.Forbidden, "operation is not configured in action policy").
			WithContext("operation", string(op))
	}
	decision, err := a.authorizer.Authorize(ctx, code, target)
	if err != nil {
		return nil, err
	}
	if !decision.IsAllowed() {
		return nil, errors.NewCode(errors.Forbidden, "authorization denied").
			WithContext("action", code).
			WithContext("reason", decision.ReasonCode)
	}
	return a.writeContext(ctx, decision)
}

// writeContext 把写约束与数据范围一并绑定到一次性派生 ctx。
//
// 两者缺一不可，这是 L3 闭环最容易断的一环：
//   - 约束定义"这次写允许落在哪些**具体资源**上"，由仓储合并进原子 WHERE；
//   - 数据范围定义"这次写允许落在哪个**范围**内"，仓储据此为新行盖范围戳
//     （applyDataScopeToEntity）并拼装范围过滤。
//
// 只投放约束而不绑定范围，声明了范围列的仓储会在写路径解析不到范围而直接
// Forbidden——装饰器看似判定通过，请求却全数被仓储拒绝。
func (a *application[T, ID]) writeContext(
	ctx context.Context,
	decision authscoped.Decision,
) (context.Context, error) {
	scopedCtx, err := a.bindEffectiveScope(ctx, decision)
	if err != nil {
		return nil, err
	}
	provider := authscoped.SingleEntityConstraint(a.config.EntityType, decision.WriteConstraint())
	return authscoped.WithConstraint(scopedCtx, provider), nil
}

// writeContextWithScope 绑定已合并的写约束与已合并的数据范围。
//
// 批量写路径逐条判定后自行合并约束与范围（见 batchWriter.authorizeAll），
// 不再持有单条 Decision，因此直接走范围绑定核心逻辑。
func (a *application[T, ID]) writeContextWithScope(
	ctx context.Context,
	constraint authscoped.WriteConstraint,
	scope authscoped.DataScope,
) (context.Context, error) {
	scopedCtx, err := a.bindEffectiveScopeValue(ctx, scope)
	if err != nil {
		return nil, err
	}
	provider := authscoped.SingleEntityConstraint(a.config.EntityType, constraint)
	return authscoped.WithConstraint(scopedCtx, provider), nil
}

// bindEffectiveScope 把「决策授权范围」与「主体可见范围」求交后绑定到 ctx。
//
// 为什么是求交而不是二选一（§4.1 第 3 条 约束只增不减）：
//   - 只用决策范围：PDP 放宽时会绕过主体自身的可见范围限制；
//   - 只用主体范围：决策针对具体资源收窄的范围会被放大回去。
//
// 两处特例：
//   - 决策没给出可用范围（典型是新建，资源尚未归属任何范围）→ 以主体范围为准，
//     由仓储在落库时盖戳；
//   - ctx 未绑定范围且未配置 ScopeResolver，但决策已给出可用范围 → 以决策为准。
//     缺 resolver 本身不是放行理由，但也不该让"PDP 已明确授权"的请求无谓失败。
//
// 注意区分两种"范围为空"：
//   - 决策本身是拒绝（EffectDeny）→ 必须 fail-closed，且优先于任何回退；
//   - 决策放行但未回显资源（动作级 allow，如列表读/新建）→ 范围为空不代表
//     拒绝，应回退到主体可见范围。用 decision.Effect 判断，而不是用派生出的
//     DataScope.Kind：后者对两种情况都退化为 ScopeDenyAll，无法区分。
func (a *application[T, ID]) bindEffectiveScope(
	ctx context.Context,
	decision authscoped.Decision,
) (context.Context, error) {
	decided := decision.DataScope()
	// PDP 显式拒绝优先于任何回退路径：无论是否已绑定主体范围或配置
	// ScopeResolver，拒绝都必须 fail-closed。
	if decision.Effect != authscoped.EffectAllow {
		return nil, errors.NewCode(errors.Forbidden,
			"authorization decision denies all data access").
			WithContext("scope", decided.Kind.String()).
			WithContext("reason", decision.ReasonCode)
	}
	return a.bindEffectiveScopeValue(ctx, decided)
}

// bindEffectiveScopeValue 在"决策已放行"的前提下把决策范围与主体范围求交后绑定。
func (a *application[T, ID]) bindEffectiveScopeValue(
	ctx context.Context,
	decided authscoped.DataScope,
) (context.Context, error) {
	_, hasBoundScope := authscoped.DataScopeFromContext(ctx)
	if a.config.ScopeResolver == nil && !hasBoundScope {
		if decided.AllowsAny() {
			return bindScope(ctx, decided)
		}
		// 两边都给不出范围：无从判定边界，只能拒绝。
		return nil, errors.NewCode(errors.Forbidden,
			"no data scope available; configure Config.ScopeResolver or have the authorizer return authorized resources")
	}
	subject, err := authscoped.ResolveDataScope(ctx, a.config.ScopeResolver)
	if err != nil {
		return nil, err
	}
	// 决策未指定范围（动作级 allow / 新建资源尚未归属任何范围）：以主体范围为准。
	if !decided.AllowsAny() {
		return bindScope(ctx, subject)
	}
	return bindScope(ctx, decided.Intersect(subject))
}

// authorizeRead 完成读路径授权并把数据范围绑定到 ctx。
//
// 范围为 ScopeDenyAll 时直接拒绝（§4.1 铁律）。
func (a *application[T, ID]) authorizeRead(ctx context.Context, op action.Operation, targets ...any) (context.Context, error) {
	code, ok := a.config.Policy.CodeFor(op)
	if !ok {
		return nil, errors.NewCode(errors.Forbidden, "operation is not configured in action policy").
			WithContext("operation", string(op))
	}
	if len(targets) > 0 {
		// 已知具体资源：走完整 PDP 判定，判据是"授权资源清单覆盖了目标"。
		decision, err := a.authorizer.Authorize(ctx, code, targets...)
		if err != nil {
			return nil, err
		}
		if !decision.IsAllowed() {
			return nil, errors.NewCode(errors.NotFound, "resource not found")
		}
		return a.bindEffectiveScope(ctx, decision)
	}

	// 列表类读取：没有具体资源可传，判据退化为**动作层面**是否允许
	// （见 Decision.IsActionAllowed）；可见性随后完全由数据范围界定。
	decision, err := a.authorizer.Authorize(ctx, code)
	if err != nil {
		return nil, err
	}
	if !decision.IsActionAllowed() {
		return nil, errors.NewCode(errors.Forbidden, "authorization denied").
			WithContext("action", code).
			WithContext("reason", decision.ReasonCode)
	}
	return a.bindEffectiveScope(ctx, decision)
}

func bindScope(ctx context.Context, scope authscoped.DataScope) (context.Context, error) {
	if !scope.AllowsAny() {
		return nil, errors.NewCode(errors.Forbidden, "data scope denies all access")
	}
	return authscoped.WithDataScope(ctx, scope)
}

// resolveBoundary 通过仓储探针解析资源边界。
//
// 这是**前置资源解析**（正常授权输入），不是事后错误分类探针。
func (a *application[T, ID]) resolveBoundary(ctx context.Context, id ID) (any, error) {
	if reader, ok := any(a.repository).(authscoped.IResourceBoundaryReader[ID]); ok {
		resource, err := reader.ResolveResourceByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return resource, nil
	}
	// 无探针时退化为先读实体再授权。读取本身受下层隔离/范围约束保护。
	entity, err := a.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return entity, nil
}

// resolveDeletedBoundary 解析**可能已软删**的目标资源边界。
//
// 审计高危操作（Restore / Purge / AuditTrail）的目标通常正是软删记录，
// 用常规探针解析会被 `deleted_at IS NULL` 过滤掉，授权还没开始就先 404。
// 按可用性依次降级，全都不可用时退回常规解析（非软删实体本就等价）。
func (a *application[T, ID]) resolveDeletedBoundary(ctx context.Context, id ID) (any, error) {
	if reader, ok := any(a.repository).(authscoped.IDeletedResourceBoundaryReader[ID]); ok {
		resource, err := reader.ResolveResourceByIDIncludingDeleted(ctx, id)
		if err != nil {
			return nil, err
		}
		return resource, nil
	}
	if getter, ok := any(a.inner).(interface {
		GetWithDeleted(ctx context.Context, id ID) (T, error)
	}); ok {
		entity, err := getter.GetWithDeleted(ctx, id)
		if err != nil {
			return nil, err
		}
		return entity, nil
	}
	return a.resolveBoundary(ctx, id)
}
