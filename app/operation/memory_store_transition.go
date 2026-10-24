package operation

import (
	"context"

	"gochen/errors"
)

// TransitionStatus 只在预期状态仍成立时推进，保留存储内的其他字段。
func (s *MemoryStore) TransitionStatus(ctx context.Context, id string, expected, next Status) (*Result, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || id == "" || !CanTransitionStatus(expected, next) {
		return nil, errors.NewCode(errors.InvalidInput, "invalid operation status transition")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, ok := s.records[id]
	if !ok || s.isIdempotencyReservationLocked(id) {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	if current.Operation.Status == expected {
		current.Operation.Status = next
	}
	return CloneResult(current), nil
}

// RecoverResult 在同一临界区中检查归属并补存，已有可见结果始终优先。
//
// 补存的新记录与 Put 同等参与容量核算：登记 FIFO 顺序并触发淘汰
// （本条记录保位），随后才绑定幂等键——保证绑定指向仍然存在的记录。
func (s *MemoryStore) RecoverResult(ctx context.Context, key string, original *Result) (*Result, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || original == nil || original.Operation.ID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "operation store and original result with ID are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.initializeLocked()
	id := original.Operation.ID
	if _, deleted := s.deleted[id]; deleted {
		return nil, errors.NewCode(errors.Conflict, "operation has been deleted").WithContext("operation_id", id)
	}
	if owner, bound := s.idempotencyKeys[key]; key != "" && bound && owner != id {
		return nil, errors.NewCode(errors.Conflict, "idempotency key is owned by another operation").WithContext("operation_id", owner)
	}
	if s.isIdempotencyReservationLocked(id) && (key == "" || s.idempotencyKeys[key] != id) {
		return nil, errors.NewCode(errors.Conflict, "recovery must complete the original idempotency reservation")
	}
	current, exists := s.records[id]
	if !exists || s.isIdempotencyReservationLocked(id) {
		current = CloneResult(original)
		s.records[id] = current
		delete(s.idempotencyReservations, id)
		s.trackRecordOrderLocked(id)
		s.evictOverflowRecordsLocked(id)
	}
	s.bindIdempotencyKeyLocked(key, id)
	return CloneResult(current), nil
}

var _ IStatusTransitionStore = (*MemoryStore)(nil)
var _ IResultRecoveryStore = (*MemoryStore)(nil)
