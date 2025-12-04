package domain

// IObject 最基础的对象接口，所有实体的根接口。
type IObject[T comparable] interface {
	// GetID 返回对象的唯一标识
	GetID() T
}

// IEntity 实体接口，在 IObject 基础上增加版本控制。
// 版本号用于乐观锁，防止并发冲突。
type IEntity[T comparable] interface {
	IObject[T]

	// GetVersion 返回实体的乐观锁版本号
	// 每次修改都应该递增版本号，用于并发冲突检测
	GetVersion() int64
}

// IValidatable 可验证接口。
// 实现此接口的实体可以验证自身状态的有效性。
type IValidatable interface {
	// Validate 验证实体状态是否有效
	// 返回 error 表示验证失败，nil 表示验证成功
	Validate() error
}
