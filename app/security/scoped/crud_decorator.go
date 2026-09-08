// Package scoped 提供 L3 安全 PEP：在 Application 编排入口完成资源授权，
// 并把写约束经受控通道投放给仓储。
//
// 设计要点：
//   - 装饰 crud.IApplication 这一宽接口，并通过 security/internal/capability
//     按内层实际能力组合外壳，保住批量写、审计扩展与装配期注入等可选能力接口；
//     批量与审计高危操作各自做资源授权，绝不裸转发（见 capability_decorator.go）；
//   - 基础 Application（crud / audited / eventsourced）对约束零感知、零依赖：
//     约束不进入任何公共接口，只经 auth/scoped 的受控 ctx 通道传递；
//   - 构造期断言仓储已声明范围列，杜绝"约束被静默丢弃、请求全部放行"。
//
// 导入约定：本包与 gochen/auth/scoped 同名，调用方按需起别名，例如
//
//	import secscoped "gochen/app/security/scoped"
package scoped

import (
	"context"
	"fmt"
	"strings"

	"gochen/auth/action"
	authscoped "gochen/auth/scoped"

	"gochen/app/crud"
	"gochen/app/query"
	"gochen/app/security/internal/capability"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/errors"
)

// Config 描述 L3 装饰所需的编排配置。
type Config struct {
	// EntityType 是写约束投放的目标实体标识（如 "order"）。
	//
	// Repo 以同一标识读取约束；Hook 内写其他实体时匹配不到，
	// 从而被 fail-closed 拦截（嵌套写防护）。
	EntityType string

	// Policy 把 CRUD 操作映射到三段式动作码。
	Policy action.OperationPolicy

	// ScopeResolver 解析读路径的数据范围。
	//
	// 为 nil 时读路径要求 ctx 已绑定范围，否则 fail-closed。
	ScopeResolver authscoped.IDataScopeResolver
}

// application 是带资源授权与约束投放的 crud.IApplication 装饰器。
type application[T domain.IEntity[ID], ID comparable] struct {
	inner      crud.IApplication[T, ID]
	authorizer authscoped.IAuthorizer
	config     Config
}

var _ crud.IApplication[domain.IEntity[int64], int64] = (*application[domain.IEntity[int64], int64])(nil)

// New 以资源授权与受控约束通道装饰 Application。
//
// 构造期校验（全部 fail-fast，把配置问题暴露在启动期）：
//   - app / authorizer 非空；
//   - EntityType 非空（否则约束无法定向投放）；
//   - Policy 中权限码合法；
//   - 底层仓储已声明范围列（防静默降级）。
func New[T domain.IEntity[ID], ID comparable](
	app crud.IApplication[T, ID],
	authorizer authscoped.IAuthorizer,
	config Config,
) (crud.IApplication[T, ID], error) {
	if app == nil {
		return nil, errors.NewCode(errors.InvalidInput, "application cannot be nil")
	}
	if authorizer == nil {
		return nil, errors.NewCode(errors.InvalidInput, "authorizer cannot be nil")
	}
	if config.EntityType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "entity type is required to target write constraints")
	}
	if err := config.Policy.Validate(); err != nil {
		return nil, err
	}
	if err := requireScopeDeclaration[T, ID](app, config.EntityType); err != nil {
		return nil, err
	}
	core := &application[T, ID]{inner: app, authorizer: authorizer, config: config}
	return composeCapabilities[T, ID](core, app), nil
}

// composeCapabilities 探测内层可选能力，为每项挂上资源授权后组合外壳。
func composeCapabilities[T domain.IEntity[ID], ID comparable](
	core *application[T, ID],
	inner crud.IApplication[T, ID],
) crud.IApplication[T, ID] {
	detected := capability.Detect[T, ID](inner)
	parts := capability.Parts[T, ID]{Core: core, Aware: detected.Aware}
	if detected.Batch != nil {
		parts.Batch = &batchWriter[T, ID]{app: core, inner: detected.Batch}
	}
	if detected.Audited != nil {
		parts.Audited = &auditedSurface[T, ID]{app: core, inner: detected.Audited}
	}
	return capability.Compose[T, ID](parts)
}

// requireScopeDeclaration 断言底层仓储确实会消费写约束，且真的执行得了约束写。
func requireScopeDeclaration[T domain.IEntity[ID], ID comparable](
	app crud.IApplication[T, ID],
	entityType string,
) error {
	repo := app.Repository()
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
	return a.writeContext(ctx, decision.WriteConstraint())
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
	constraint authscoped.WriteConstraint,
) (context.Context, error) {
	scopedCtx, err := a.bindEffectiveScope(ctx, constraint.DataScope())
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
//   - 未配置 ScopeResolver 但决策已给出可用范围 → 以决策为准。缺 resolver 本身
//     不是放行理由，但也不该让"PDP 已明确授权"的请求无谓失败。
func (a *application[T, ID]) bindEffectiveScope(
	ctx context.Context,
	decided authscoped.DataScope,
) (context.Context, error) {
	if a.config.ScopeResolver == nil {
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
		return a.bindEffectiveScope(ctx, decision.DataScope())
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
	return a.bindEffectiveScope(ctx, decision.DataScope())
}

func bindScope(ctx context.Context, scope authscoped.DataScope) (context.Context, error) {
	if !scope.AllowsAny() {
		return nil, errors.NewCode(errors.Forbidden, "data scope denies all access")
	}
	return authscoped.WithDataScope(ctx, scope)
}

// --- IWriter：PDP 判定 → 构造 IConstraintProvider → 经通道投放 ---

func (a *application[T, ID]) Create(ctx context.Context, e T) error {
	scopedCtx, err := a.authorizeWrite(ctx, action.OpCreate, e)
	if err != nil {
		return err
	}
	return a.inner.Create(scopedCtx, e)
}

func (a *application[T, ID]) Update(ctx context.Context, e T) error {
	// 授权输入必须是**仓储里的真实边界**，不能是客户端提交的实体。
	// 否则 PDP 看到的 owner / scope / revision 全由请求体决定：范围与租户虽然
	// 会被仓储的原子 WHERE 兜住，但 owner 这类不下推的属性一旦被策略引用，
	// 伪造归属即可让 PDP 放行一次本不该允许的写。
	target, err := a.resolveBoundary(ctx, e.GetID())
	if err != nil {
		return err
	}
	scopedCtx, err := a.authorizeWrite(ctx, action.OpUpdate, target)
	if err != nil {
		return err
	}
	return a.inner.Update(scopedCtx, e)
}

func (a *application[T, ID]) Delete(ctx context.Context, id ID) error {
	// 删除只有 ID，需先解析目标资源边界作为授权输入。
	target, err := a.resolveBoundary(ctx, id)
	if err != nil {
		return err
	}
	scopedCtx, err := a.authorizeWrite(ctx, action.OpDelete, target)
	if err != nil {
		return err
	}
	return a.inner.Delete(scopedCtx, id)
}

// resolveBoundary 通过仓储探针解析资源边界。
//
// 这是**前置资源解析**（正常授权输入），不是事后错误分类探针。
func (a *application[T, ID]) resolveBoundary(ctx context.Context, id ID) (any, error) {
	if reader, ok := any(a.inner.Repository()).(authscoped.IResourceBoundaryReader[ID]); ok {
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
	if reader, ok := any(a.inner.Repository()).(authscoped.IDeletedResourceBoundaryReader[ID]); ok {
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

// --- IReader ---

func (a *application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	target, err := a.resolveBoundary(ctx, id)
	if err != nil {
		return zero, err
	}
	scopedCtx, err := a.authorizeRead(ctx, action.OpRead, target)
	if err != nil {
		return zero, err
	}
	return a.inner.Get(scopedCtx, id)
}

func (a *application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	// 与 L1 装饰器一致用 read 动作码：Exists 问的是"这条记录在不在"，
	// 属于单条读语义。两层用不同动作码会让一次 Exists 同时要求 read 与 list 两个权限。
	scopedCtx, err := a.authorizeRead(ctx, action.OpRead)
	if err != nil {
		return false, err
	}
	return a.inner.Exists(scopedCtx, id)
}

func (a *application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.List(scopedCtx, offset, limit)
}

func (a *application[T, ID]) Count(ctx context.Context) (int64, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return 0, err
	}
	return a.inner.Count(scopedCtx)
}

func (a *application[T, ID]) ListByQuery(ctx context.Context, quer *query.QueryRequest) ([]T, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.ListByQuery(scopedCtx, quer)
}

func (a *application[T, ID]) ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return nil, err
	}
	return a.inner.ListPage(scopedCtx, request)
}

func (a *application[T, ID]) CountByQuery(ctx context.Context, quer *query.QueryRequest) (int64, error) {
	scopedCtx, err := a.authorizeRead(ctx, action.OpList)
	if err != nil {
		return 0, err
	}
	return a.inner.CountByQuery(scopedCtx, quer)
}

// --- 非请求路径能力：原样转发 ---

func (a *application[T, ID]) Repository() domcrud.IRepository[T, ID] { return a.inner.Repository() }

func (a *application[T, ID]) QueryRepository() (domcrud.IQueryRepository[T, ID], bool) {
	return a.inner.QueryRepository()
}

func (a *application[T, ID]) Validate(entity T) error { return a.inner.Validate(entity) }

func (a *application[T, ID]) Config() *crud.ServiceConfig { return a.inner.Config() }
