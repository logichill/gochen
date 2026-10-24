package scoped

import (
	"context"

	"gochen/app/internal/depcheck"
	"gochen/app/internal/writeflow"
	"gochen/auth/action"
	authscoped "gochen/auth/scoped"
	"gochen/domain"
	"gochen/errors"
)

// L3 可选能力面的资源授权。
//
// 批量与审计高危操作是与 Create/Update/Delete 平级的写入口，
// 不能原样转发给内层——那等于给未授权调用留一条绕过 PDP 的旁路。

// batchWriter 是带资源授权与约束投放的批量写实现。
//
// 约束按 §2.3.2 逐条构造：一次批量里每个目标资源各自经 PDP 判定，
// 汇总为一个覆盖全部目标的 IConstraintProvider 后一次性投放，
// 由仓储在同一事务内逐条下推原子约束。
type batchWriter[T domain.IEntity[ID], ID comparable] struct {
	app   *application[T, ID]
	inner interface {
		CreateAll(ctx context.Context, entities []T) error
		UpdateAll(ctx context.Context, entities []T) error
		DeleteAll(ctx context.Context, ids []ID) error
	}
}

func (b *batchWriter[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if err := b.requireBatchSize(len(entities)); err != nil {
		return err
	}
	if err := requireNonNilEntities(entities); err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpCreate, toTargets(entities))
	if err != nil {
		return err
	}
	return b.inner.CreateAll(scopedCtx, entities)
}

func (b *batchWriter[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if err := b.requireBatchSize(len(entities)); err != nil {
		return err
	}
	if err := requireNonNilEntities(entities); err != nil {
		return err
	}
	// 与内层 crud Application 同规则：同批次重复 ID 属参数错误，应在授权前拒绝；
	// 否则批量探针的 IN 查询去重后数量不匹配，会被误报为 Forbidden。
	seenEntities := make(map[ID]struct{}, len(entities))
	ids := make([]ID, 0, len(entities))
	for i := range entities {
		id := entities[i].GetID()
		if _, ok := seenEntities[id]; ok {
			return errors.NewCode(errors.InvalidInput, "duplicate entity id in batch update").
				WithContext("id", id)
		}
		seenEntities[id] = struct{}{}
		ids = append(ids, id)
	}
	// 与单条 Update 同理：授权输入取自仓储的真实边界，不取客户端实体。
	targets, err := b.boundariesOf(ctx, ids)
	if err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpUpdate, targets)
	if err != nil {
		return err
	}
	return b.inner.UpdateAll(scopedCtx, entities)
}

func (b *batchWriter[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if err := b.requireBatchSize(len(ids)); err != nil {
		return err
	}
	// 与内层 crud Application 同规则：先去重，避免批量探针对重复 ID 仅返回
	// 一条记录，导致 len(resources) != len(ids) 被误报为 Forbidden，破坏幂等删除契约。
	ids = writeflow.DedupIDs(ids)
	targets, err := b.boundariesOf(ctx, ids)
	if err != nil {
		return err
	}
	scopedCtx, err := b.authorizeAll(ctx, action.OpDelete, targets)
	if err != nil {
		return err
	}
	return b.inner.DeleteAll(scopedCtx, ids)
}

// requireBatchSize 在**授权之前**卡住批量规模。
//
// 内层 Application 也校验同一个上限，但它在 L3 转发之后才执行，而 L3 的
// 单次批量成本是 N 次边界解析 + N 次 PDP 判定 + N 条授权审计。等内层再拒，
// 这些成本已经全部付掉了——请求体上限（默认 10MB）折算成 ID 数量是六位数，
// 足以把一次必然失败的请求放大成一次数据库风暴。构造时读取内层配置，
// 标准应用保持同一个上限；未提供配置的自定义应用使用默认上限。
func (b *batchWriter[T, ID]) requireBatchSize(size int) error {
	return writeflow.ValidateBatchSize(size, b.app.maxBatchSize)
}

// boundariesOf 解析一批 ID 在仓储中的真实授权边界，顺序与入参一致。
//
// 优先走批量探针（一次 IN 查询）；仓储只提供单条探针或完全没有探针时，
// 逐条降级到 resolveBoundary——降级路径的语义与批量路径等价，只是往返更多。
func (b *batchWriter[T, ID]) boundariesOf(ctx context.Context, ids []ID) ([]any, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if reader, ok := any(b.app.repository).(authscoped.IBatchResourceBoundaryReader[ID]); ok {
		resources, err := reader.ResolveResourcesByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		if len(resources) != len(ids) {
			// 边界数量对不上就无法保证"每个目标都被判定过"，只能整批拒绝。
			return nil, errors.NewCode(errors.Forbidden, "resource boundary count does not match the batch targets").
				WithContext("expected", len(ids)).
				WithContext("actual", len(resources))
		}
		targets := make([]any, 0, len(resources))
		for i := range resources {
			targets = append(targets, resources[i])
		}
		return targets, nil
	}
	targets := make([]any, 0, len(ids))
	for _, id := range ids {
		target, err := b.app.resolveBoundary(ctx, id)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// authorizeAll 逐条判定并合并约束。
//
// 任一条不通过即整批拒绝——批量不是"尽力而为"，部分放行会让调用方
// 以为整批都被授权了。
func (b *batchWriter[T, ID]) authorizeAll(ctx context.Context, op action.Operation, targets []any) (context.Context, error) {
	if len(targets) == 0 {
		return ctx, nil
	}
	code, ok := b.app.config.Policy.CodeFor(op)
	if !ok {
		return nil, errors.NewCode(errors.Forbidden, "operation is not configured in action policy").
			WithContext("operation", string(op))
	}
	merged := authscoped.WriteConstraint{}
	mergedScope := authscoped.Decision{Effect: authscoped.EffectAllow}.DataScope()
	for i, target := range targets {
		decision, err := b.app.authorizer.Authorize(ctx, code, target)
		if err != nil {
			return nil, err
		}
		if !decision.IsAllowed() {
			return nil, errors.NewCode(errors.Forbidden, "authorization denied").
				WithContext("action", code).
				WithContext("reason", decision.ReasonCode)
		}
		// 约束只增不减（§4.1 第 3 条）：逐条累加，不覆盖已有项。
		granted := decision.WriteConstraint().Normalize()
		merged.Resources = append(merged.Resources, granted.Resources...)
		// 范围同样只增不减：逐条求交，任一条收窄则整批收窄到交集。
		targetScope := granted.DataScope()
		if targetScope.AllowsAny() {
			if i == 0 || !mergedScope.AllowsAny() {
				mergedScope = targetScope
			} else {
				mergedScope = mergedScope.Intersect(targetScope)
			}
		}
	}
	if merged.IsEmpty() {
		return nil, errors.NewCode(errors.Forbidden, "batch write is not authorized for any target").
			WithContext("action", code)
	}
	// 与单条写路径共用同一套 ctx 构造：约束与数据范围必须一起绑定，
	// 否则声明了范围列的仓储会在写路径解析不到范围而整批拒绝。
	return b.app.writeContextWithScope(ctx, merged, mergedScope)
}

func toTargets[T any](entities []T) []any {
	targets := make([]any, 0, len(entities))
	for i := range entities {
		targets = append(targets, entities[i])
	}
	return targets
}

// requireNonNilEntities 拒绝批量载荷中的 typed nil 实体：授权与写入阶段都会
// 解引用实体，nil 元素会直接 panic 逃逸出调用栈。
func requireNonNilEntities[T any](entities []T) error {
	for i := range entities {
		if depcheck.IsNil(entities[i]) {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").
				WithContext("index", i)
		}
	}
	return nil
}
