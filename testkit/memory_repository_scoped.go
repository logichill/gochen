package testkit

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gochen/auth/scoped"
	"gochen/domain"
	"gochen/domain/crud"
	"gochen/errors"
)

// L3 受控约束通道的内存实现。
//
// 与 runtime/db/orm/repo 的 SQL 实现保持**相同语义**：
//   - 范围能力由构造期显式声明启用（WithMemoryScope），未声明即不启用；
//   - 声明后所有写路径必须从受控通道取到本实体类型的写约束，
//     取不到即 errors.Forbidden——不经 L3 PEP、或异步丢了派生 ctx 都会被拦下；
//   - 约束授权的资源必须与本次写的目标 ID 一致；
//   - update / delete 的约束必须携带 revision，作为乐观锁基线：
//     调用方实体版本与基线不符 → errors.Concurrency（读到了过期快照），
//     仓储中的行版本与基线不符 → errors.Conflict（等价 SQL 侧 WHERE version 未命中）；
//   - create / update 会把约束授权的租户盖到实体的 ITenantEntity 槽位上，
//     实体已带冲突租户 → errors.Forbidden。
//
// 与 SQL 侧的已知形态差异（内存仓储没有"列"的概念所致）：
//   - 约束的 ManagedScopeID 不落实体——内存仓储读路径没有范围过滤，
//     单方面盖范围戳会造成"看似有范围防护"的假象；
//   - 不做版本推进与对齐——内存仓储整体由调用方管理实体版本（无约束写同样如此）。
//
// 有了这套实现，L3 装配（security.Scoped 会断言 IScopeDeclarationProbe）
// 才能在不起数据库的情况下被完整测试。

// WithMemoryScope 显式启用 L3 范围能力，entityType 必须与
// app/security/scoped 的 Config.EntityType 一致，否则约束投放对不上。
func WithMemoryScope[T domain.IEntity[ID], ID comparable](entityType string) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		if r == nil {
			return
		}
		entityType = strings.TrimSpace(entityType)
		if entityType == "" {
			panic("testkit: WithMemoryScope requires a non-empty entity type")
		}
		r.scopeEntityType = entityType
	}
}

// HasScopeDeclaration 实现 scoped.IScopeDeclarationProbe。
func (r *MemoryRepository[T, ID]) HasScopeDeclaration() bool {
	return r != nil && r.scopeEntityType != ""
}

// ResourceKind 实现 scoped.IResourceKindProbe，让装配层能在构造期核对
// Config.EntityType 与本仓储读取约束所用的标识是否一致。
func (r *MemoryRepository[T, ID]) ResourceKind() string {
	if r == nil {
		return ""
	}
	return r.scopeEntityType
}

// ValidateWriteConstraintSupport 实现 scoped.IConstraintWriteProbe。
// 内存仓储的写入结果由本进程直接判定，不依赖额外驱动能力。
func (r *MemoryRepository[T, ID]) ValidateWriteConstraintSupport() error { return nil }

// constraintGate 与 SQL 侧同名同义：未声明范围列则不启用；
// 声明了却取不到约束则 fail-closed。
func (r *MemoryRepository[T, ID]) constraintGate(ctx context.Context) (scoped.WriteConstraint, bool, error) {
	if !r.HasScopeDeclaration() {
		return scoped.WriteConstraint{}, false, nil
	}
	constraint, ok := scoped.ConstraintFrom(ctx, r.scopeEntityType)
	if ok {
		return constraint, true, nil
	}
	reason := "no write constraint provider in context"
	if scoped.HasConstraintProvider(ctx) {
		reason = "write constraint provider does not cover this entity type"
	}
	return scoped.WriteConstraint{}, false,
		errors.NewCode(errors.Forbidden, "write is not authorized by any write constraint").
			WithContext("entity_type", r.scopeEntityType).
			WithContext("reason", reason)
}

// memoryConstrainedWrite 汇总单条受约束写所需的核对参数（批量路径按 ID 索引）。
type memoryConstrainedWrite struct {
	resource scoped.ResourceConstraint
	expected uint64
}

// requireConstraintFor 校验约束确实授权了该目标 ID，返回命中的资源约束。
//
// 第二个返回值为 false 表示本仓储未声明范围能力（L0/L1/L2），约束为零值。
func (r *MemoryRepository[T, ID]) requireConstraintFor(ctx context.Context, id ID) (scoped.ResourceConstraint, bool, error) {
	constraint, constrained, err := r.constraintGate(ctx)
	if err != nil || !constrained {
		return scoped.ResourceConstraint{}, false, err
	}
	resource, err := constraint.RequireResource(r.scopeEntityType, formatMemoryResourceID(id))
	if err != nil {
		return scoped.ResourceConstraint{}, false, err
	}
	return resource, true, nil
}

// requireVersionedConstraint 解析约束 revision 作为 update / delete 的乐观锁基线。
//
// 与 SQL 侧 requireVersionedConstraint 同口径：revision 必填，
// 缺失或非法视为装配错误（InvalidInput），而不是静默跳过版本校验。
func (r *MemoryRepository[T, ID]) requireVersionedConstraint(resource scoped.ResourceConstraint) (uint64, error) {
	revision := strings.TrimSpace(resource.Revision)
	if revision == "" {
		return 0, errors.NewCode(errors.InvalidInput, "write constraint revision is required")
	}
	version, err := strconv.ParseUint(revision, 10, 64)
	if err != nil {
		return 0, errors.NewCode(errors.InvalidInput, "write constraint revision must be an unsigned integer").
			WithContext("revision", revision)
	}
	return version, nil
}

// checkEntityVersion 比对调用方实体版本与约束基线。
//
// 版本省略（0）时跳过（与 SQL 侧一致）；不一致说明调用方读到的是过期快照，
// 报 Concurrency 而不是 Forbidden——客户端应去重试，而不是以为自己无权。
func (r *MemoryRepository[T, ID]) checkEntityVersion(entity T, expected uint64) error {
	if version := entity.GetVersion(); version != 0 && version != expected {
		return errors.NewCode(errors.Concurrency, "concurrent modification detected").
			WithContext("expected_version", expected).
			WithContext("entity_version", version)
	}
	return nil
}

// checkStoredVersion 比对仓储中的行版本与约束基线。
//
// 等价 SQL 侧 `WHERE version = ?` 未命中后的 Conflict 归因：授权与写入之间
// 行版本已被其他写入推进，本次约束写必须整体失败。
func (r *MemoryRepository[T, ID]) checkStoredVersion(existing T, expected uint64) error {
	if version := existing.GetVersion(); version != expected {
		return errors.NewCode(errors.Conflict, "record revision mismatch").
			WithContext("expected_version", expected).
			WithContext("actual_version", version)
	}
	return nil
}

// stampConstraintTenant 把约束授权的租户盖到实体的 ITenantEntity 槽位上，
// 对齐 SQL 侧 applyConstraintToEntity 的租户对齐语义：
// 实体已带不同租户 → Forbidden（归属不可被写入篡改）。
func (r *MemoryRepository[T, ID]) stampConstraintTenant(entity T, resource scoped.ResourceConstraint) error {
	tenant := strings.TrimSpace(resource.TenantID)
	if tenant == "" {
		return nil
	}
	aware, ok := any(entity).(crud.ITenantEntity[ID])
	if !ok {
		return errors.NewCode(errors.Forbidden, "write constraint tenant_id cannot be enforced for this entity").
			WithContext("tenant_id", tenant)
	}
	if current := strings.TrimSpace(aware.GetTenantID()); current != "" && current != tenant {
		return errors.NewCode(errors.Forbidden, "write constraint does not authorize the target tenant_id").
			WithContext("tenant_id", current).
			WithContext("authorized_tenant_id", tenant)
	}
	aware.SetTenantID(tenant)
	return nil
}

// formatMemoryResourceID 与 SQL 侧 formatResourceID 保持一致：零值 ID 视为"未绑定具体资源"。
func formatMemoryResourceID[ID comparable](id ID) string {
	var zero ID
	if id == zero {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(id))
}
