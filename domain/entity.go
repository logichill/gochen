package domain

import "time"

// IEntity 实体接口，包含唯一标识与乐观锁版本控制。
// 版本号用于乐观锁，防止并发冲突。
type IEntity[T comparable] interface {
	// GetID 返回对象的唯一标识
	GetID() T

	// GetVersion 返回实体的乐观锁版本号
	// 每次修改都应该递增版本号，用于并发冲突检测。
	// 约定：版本号从 0 开始，自增且不为负。
	GetVersion() uint64
}

// ISoftDeletable 软删除能力（最小语义）。
//
// 说明：
// - 该接口仅表达“删除态 + 删除时间 + 状态机”，不强制要求记录操作人；
// - 若业务需要记录删除操作人（DeletedBy），请使用 `domain.ISoftDeletableBy`。
type ISoftDeletable interface {
	GetDeletedAt() *time.Time
	IsDeleted() bool

	// SoftDelete 将实体标记为软删除。
	// 说明：
	// - 该接口表达的是“最小软删语义”，不包含操作者（by/operator）归因；
	// - 若业务需要记录删除操作人（DeletedBy）等审计信息，请使用 `domain.ISoftDeletableBy` 能力。
	SoftDelete(at time.Time) error

	// Restore 恢复被软删除的实体。
	Restore() error
}

// ISettableID 可选的 ID 回填接口。
//
// 说明：
// - `IEntity[T]` 仅要求 GetID，不要求 SetID；
// - 默认仓储在需要“写前生成 ID”（例如使用 Snowflake/UUID）时，会通过运行时类型断言检测该能力并写入；
// - 若实体不实现该接口，则仓储不会尝试回填 ID（例如使用数据库自增或由调用方自行赋值）。
type ISettableID[T comparable] interface {
	// SetID 设置对象的唯一标识
	SetID(id T)
}

// ISettableVersion 可选的版本回写接口。
//
// 说明：
//   - 实现该接口表明实体的乐观锁版本可由仓储回写；
//   - 嵌入 `domain/crud.Entity` 的实体默认获得本能力；
//   - Runtime ORM 仓储默认要求该能力及版本列，普通实体可显式声明 WithoutVersioning。
//     审计实体持续要求版本保护，具体构造规则由仓储实现声明。
type ISettableVersion interface {
	// SetVersion 设置实体的乐观锁版本号。
	SetVersion(version uint64)
}

// IValidatable 表达实体自身始终成立的领域不变量。
//
// 该校验不得依赖 HTTP 请求、数据库或其他外部服务；需要上下文或外部数据的应用策略
// 应通过 validate.IValidator 注入应用层。协议层请求体格式校验则属于 api/rest BodyOptions。
type IValidatable interface {
	// Validate 验证实体当前状态是否满足领域不变量。
	Validate() error
}

// IDomainEvent 领域事件接口。
// 领域层仅关注事件本身的语义，不关心传输信封与存储细节。
type IDomainEvent interface {
	// EventType 返回领域事件类型标识。
	// 建议使用稳定的枚举字符串，便于路由与演进。
	EventType() string
}
