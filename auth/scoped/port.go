package scoped

import "context"

// IResourceBoundaryReader 是"按资源 ID 反查授权边界"的仓储探针。
//
// 用途分两类，必须严格区分：
//
//  1. **前置资源解析**（read/update/delete 路由在进入 Application 前需要知道
//     目标资源属于哪个 scope），这是正常的授权输入；
//  2. **事后错误分类**（约束写返回 RowsAffected == 0 时区分 404/403/409）。
//
// 铁律：第 2 类探针**绝不参与前置安全判定**。
// 授权完全由 Repo 依据其声明的安全列在存储层原子执行，因此无 TOCTOU 风险。
// 实施者严禁把探针逻辑挪到写操作之前充当"先查后判断"。
type IResourceBoundaryReader[ID comparable] interface {
	ResolveResourceByID(ctx context.Context, id ID) (Resource, error)
}

// IBatchResourceBoundaryReader 一次性解析多个资源的授权边界。
//
// 为什么值得单列一个探针：批量写的授权输入同样必须取自仓储真实边界，
// 而逐条调用 IResourceBoundaryReader 会退化成 N 次单行查询——批量上限
// 默认 1000，等于一次批量写先打 1000 个来回。语义与单条版完全一致
// （同样不套用 L3 DataScope、同样强制 L2 tenant 隔离、同样排除软删记录），
// 差别只在往返次数，因此实现方要么两个都实现，要么只实现单条版由调用方降级。
//
// 实现约定：返回结果必须与入参 ids **按序一一对应**；任一 ID 解析不到即
// 返回 NotFound，不得静默跳过——少一条边界就等于少一次授权判定。
type IBatchResourceBoundaryReader[ID comparable] interface {
	ResolveResourcesByID(ctx context.Context, ids []ID) ([]Resource, error)
}

// IDeletedResourceBoundaryReader 解析**包含软删记录**在内的授权边界。
//
// 为什么必须与 IResourceBoundaryReader 分开：审计高危操作（Restore / Purge /
// AuditTrail）的目标恰恰是已软删的记录，用排除软删的常规探针解析必然 NotFound，
// 授权还没开始就先 404 了。两个探针语义不同，不能合并成一个带 bool 开关的方法——
// 调用方传错开关就是一次静默的越权面扩大。
type IDeletedResourceBoundaryReader[ID comparable] interface {
	ResolveResourceByIDIncludingDeleted(ctx context.Context, id ID) (Resource, error)
}

// IConstraintWriteProbe 让装配层在**构造期**确认仓储真的能执行约束写。
//
// 约束写依赖底层驱动返回受影响行数（RowsAffected）来判定命中与否；
// 驱动不支持时约束会退化为无法验证的盲写。该预检必须发生在启动期，
// 否则问题要等到某条写路径第一次被调用才暴露。
type IConstraintWriteProbe interface {
	ValidateWriteConstraintSupport() error
}

// IScopeDeclarationProbe 让装配层能在**构造期**确认仓储确实声明了范围列。
//
// 防静默降级：L3 装饰器会把写约束投放进 ctx，
// 但只有声明了范围列的 Repo 才会去消费它。若把未声明的普通 Repo 装进 L3，
// 约束会被静默丢弃、请求全部放行——这是最危险的失败模式。
// 因此 L3 装配时断言本接口，未实现或未声明即启动期 fail-fast。
type IScopeDeclarationProbe interface {
	// HasScopeDeclaration 返回仓储是否已声明范围列（如 managed_scope_id）。
	HasScopeDeclaration() bool
}

// IResourceKindProbe 暴露仓储用于匹配写约束的资源类型标识。
//
// 约束是**按实体类型定向投放**的（见 provider.go）：装饰器以 Config.EntityType
// 投放，仓储以自己的资源类型读取，两者不一致时约束恒匹配不到，所有写都会
// fail-closed 成 Forbidden。这类拼写/命名不一致必须在装配期就暴露——
// 否则表现为"配置看着都对，但一写就 403"，极难定位。
type IResourceKindProbe interface {
	ResourceKind() string
}
