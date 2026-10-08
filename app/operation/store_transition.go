package operation

import "context"

// IStatusTransitionStore 原子推进已有 operation 的状态，不覆盖其他结果字段。
// 当前状态与 expected 不符时不写入，返回当前结果；未命中返回 NotFound。
// 实现只接受 accepted → processing/终态、processing → 终态，拒绝回退及终态改写。
type IStatusTransitionStore interface {
	TransitionStatus(ctx context.Context, id string, expected, next Status) (*Result, error)
}

// IResultRecoveryStore 补存业务已提交但未保存的原始结果，不能用全量覆盖实现。
// 相同 ID 已有可见结果时保留并返回它；否则插入或完成属于该 ID 的幂等预占。
// 非空 key 必须与结果原子绑定；被其他 ID 占用、预占不属于该 key 或存在删除标记时返回 Conflict。
// 不支持幂等键的实现必须对非空 key 返回 Unsupported，不能忽略 key。
type IResultRecoveryStore interface {
	RecoverResult(ctx context.Context, key string, original *Result) (*Result, error)
}
