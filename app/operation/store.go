package operation

import (
	"context"

	"gochen/errors"
)

// IStore 抽象 operation envelope 的最小持久化能力。
type IStore interface {
	// Get 按 operation id 读取结果；未命中必须返回可被 IsStoreNotFound 识别的错误。
	Get(ctx context.Context, id string) (*Result, error)
	// Put 全量保存或替换；调用方负责协调写入，状态推进和补存应使用条件写入接口。
	Put(ctx context.Context, result *Result) error
	Delete(ctx context.Context, id string) error
}

// IIdempotencyStore 可选扩展：按业务幂等键复用 tracked operation 结果。
//
// GetByIdempotencyKey 未命中必须返回可被 IsStoreNotFound 识别的错误。
//
// PutWithIdempotencyKey 必须以原子方式同时保存 result 并绑定 key：
// - key 已绑定到另一个仍存在的 operation ID 时返回 Conflict，不得覆盖；
// - 相同 operation ID 允许补存或更新结果，调用方负责串行化该 operation 的结果更新；
// - key 绑定成功后 GetByIdempotencyKey 必须能读到同一份 result。
//
// Runner 的进程内 singleflight 只覆盖同一 Runner 实例；跨进程/多实例幂等依赖
// store 实现上述原子写入契约。
type IIdempotencyStore interface {
	GetByIdempotencyKey(ctx context.Context, key string) (*Result, error)
	PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error
}

// IsStoreNotFound 判断 store 读取错误是否表示未命中。
//
// 物理存储实现必须在适配器边界把驱动级未命中错误归一化为 errors.NotFound。
func IsStoreNotFound(err error) bool {
	return errors.Is(err, errors.NotFound)
}

// IIdempotencyReservationStore 可选扩展：在业务 handler 执行前预占幂等键。
//
// ReserveIdempotencyKey 必须以原子方式绑定 key 与 operationID：
// - key 已绑定到仍存在的 operation 时返回 Conflict；
// - key 绑定到缺失 operation 时可清理旧绑定并重新占用；
// - 占用成功后后续 PutWithIdempotencyKey 必须允许同一 operationID 写入最终 result。
//
// 实现该接口后，Runner 可以在跨进程/多实例并发时避免同一幂等键重复执行 handler。
type IIdempotencyReservationStore interface {
	ReserveIdempotencyKey(ctx context.Context, key string, operationID string) error
}

// IIdempotencyReservationReleaseStore 可选扩展：释放尚未写入最终结果的幂等键预占。
//
// ReleaseIdempotencyKey 只应删除同一 operationID 对应的预占占位；如果 key 已绑定到最终
// result，或绑定到其他 operation，必须保持原状。
type IIdempotencyReservationReleaseStore interface {
	ReleaseIdempotencyKey(ctx context.Context, key string, operationID string) error
}
