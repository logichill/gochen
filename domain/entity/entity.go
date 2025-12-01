// Package entity 定义领域实体的核心接口体系
//
// 设计原则：
// 1. 接口最小化 - 每个接口只包含必需的方法
// 2. 组合优于继承 - 通过接口组合构建复杂类型
// 3. 泛型支持 - 提供类型安全的 ID 类型
package entity

import "time"

// IObject 最基础的对象接口，所有实体的根接口
// 使用泛型支持不同的 ID 类型（int64、string、UUID等）
type IObject[T comparable] interface {
	// GetID 返回对象的唯一标识
	GetID() T
}

// IEntity 实体接口，在 IObject 基础上增加版本控制
// 版本号用于乐观锁，防止并发冲突
//
// 注意：此 Version 与 eventing.Event.Version 语义不同：
//   - Entity.Version: 乐观锁版本号，用于检测并发修改冲突
//   - Event.Version: 事件在聚合事件流中的序号，用于事件排序和重放
type IEntity[T comparable] interface {
	IObject[T]

	// GetVersion 返回实体的乐观锁版本号
	// 每次修改都应该递增版本号，用于并发冲突检测
	GetVersion() int64
}

// IAuditable 审计追踪接口
// 实现此接口的实体可以记录创建和修改信息
type IAuditable interface {
	// 创建信息
	GetCreatedAt() time.Time
	GetCreatedBy() string

	// 最后修改信息
	GetUpdatedAt() time.Time
	GetUpdatedBy() string

	// 设置审计信息（由基础设施层调用）
	SetCreatedInfo(by string, at time.Time)
	SetUpdatedInfo(by string, at time.Time)
}

// ISoftDeletable 软删除接口
// 实现此接口的实体支持逻辑删除而非物理删除
type ISoftDeletable interface {
	// GetDeletedAt 返回删除时间，nil 表示未删除
	GetDeletedAt() *time.Time

	// GetDeletedBy 返回删除操作者
	GetDeletedBy() *string

	// IsDeleted 判断是否已删除
	IsDeleted() bool

	// SoftDelete 执行软删除
	SoftDelete(by string, at time.Time) error

	// Restore 恢复已删除的实体
	Restore() error
}

// IValidatable 可验证接口
// 实现此接口的实体可以验证自身状态的有效性
type IValidatable interface {
	// Validate 验证实体状态是否有效
	// 返回 error 表示验证失败，nil 表示验证成功
	Validate() error
}

// IAuditedEntity 带审计与软删除能力的实体接口
// 适用于需要审计追踪、软删/恢复与状态校验的场景
type IAuditedEntity[T comparable] interface {
	IEntity[T]
	IAuditable
	ISoftDeletable
	IValidatable
}

// IStringEntity 字符串 ID 类型的审计实体
type IStringEntity interface {
	IAuditedEntity[string]
}

// IInt64Entity 整数 ID 类型的审计实体
type IInt64Entity interface {
	IAuditedEntity[int64]
}

// Entity 通用审计实体字段（用于嵌入）
// 默认使用 int64 作为主键类型
type Entity struct {
	ID        int64      `json:"id" gorm:"primaryKey"`
	Version   int64      `json:"version" gorm:"default:1"`
	CreatedAt time.Time  `json:"created_at" gorm:"autoCreateTime"`
	CreatedBy string     `json:"created_by"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" gorm:"index"`
	DeletedBy *string    `json:"deleted_by,omitempty"`
}

// GetID 实现 IObject 接口
func (e *Entity) GetID() int64 {
	return e.ID
}

// GetVersion 实现 IEntity 接口
func (e *Entity) GetVersion() int64 {
	return e.Version
}

// GetCreatedAt 实现 IAuditable 接口
func (e *Entity) GetCreatedAt() time.Time {
	return e.CreatedAt
}

// GetCreatedBy 实现 IAuditable 接口
func (e *Entity) GetCreatedBy() string {
	return e.CreatedBy
}

// GetUpdatedAt 实现 IAuditable 接口
func (e *Entity) GetUpdatedAt() time.Time {
	return e.UpdatedAt
}

// GetUpdatedBy 实现 IAuditable 接口
func (e *Entity) GetUpdatedBy() string {
	return e.UpdatedBy
}

// SetCreatedInfo 实现 IAuditable 接口
func (e *Entity) SetCreatedInfo(by string, at time.Time) {
	e.CreatedBy = by
	e.CreatedAt = at
}

// SetUpdatedInfo 实现 IAuditable 接口
func (e *Entity) SetUpdatedInfo(by string, at time.Time) {
	e.UpdatedBy = by
	e.UpdatedAt = at
	e.Version++
}

// GetDeletedAt 实现 ISoftDeletable 接口
func (e *Entity) GetDeletedAt() *time.Time {
	return e.DeletedAt
}

// GetDeletedBy 实现 ISoftDeletable 接口
func (e *Entity) GetDeletedBy() *string {
	return e.DeletedBy
}

// IsDeleted 实现 ISoftDeletable 接口
func (e *Entity) IsDeleted() bool {
	return e.DeletedAt != nil
}

// SoftDelete 实现 ISoftDeletable 接口
func (e *Entity) SoftDelete(by string, at time.Time) error {
	if e.IsDeleted() {
		return ErrAlreadyDeleted
	}
	e.DeletedAt = &at
	e.DeletedBy = &by
	e.Version++
	return nil
}

// Restore 实现 ISoftDeletable 接口
func (e *Entity) Restore() error {
	if !e.IsDeleted() {
		return ErrNotDeleted
	}
	e.DeletedAt = nil
	e.DeletedBy = nil
	e.Version++
	return nil
}

// 常见错误
var (
	ErrAlreadyDeleted = &EntityError{Code: "ALREADY_DELETED", Message: "entity is already deleted"}
	ErrNotDeleted     = &EntityError{Code: "NOT_DELETED", Message: "entity is not deleted"}
	ErrInvalidVersion = &EntityError{Code: "INVALID_VERSION", Message: "entity version mismatch"}
)

// EntityError 实体错误
type EntityError struct {
	Code    string
	Message string
}

func (e *EntityError) Error() string {
	return e.Message
}
