package operation

import (
	"context"

	"gochen/errors"
)

// GetByIdempotencyKey 按幂等键读取已有 tracked operation 结果。
func (s *MemoryStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory operation store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	id, ok := s.idempotencyKeys[key]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("idempotency_key", key)
	}
	result, ok := s.records[id]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	if s.isIdempotencyReservationLocked(id) {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	return CloneResult(result), nil
}

// PutWithIdempotencyKey 原子保存 operation 并把幂等键绑定到该 operation。
//
// 容量淘汰不会触及本调用刚写入的记录（protectID），因此绑定发生时
// 目标记录必然仍存在，不会形成指向已删除记录的幽灵绑定——
// 否则同键重放会因 GetByIdempotencyKey 返回 NotFound 而重新执行业务回调，
// 破坏"至多一次"幂等契约。
func (s *MemoryStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "memory operation store is nil")
	}
	if key == "" {
		return s.Put(ctx, result)
	}
	if result == nil {
		return errors.NewCode(errors.InvalidInput, "operation result cannot be nil")
	}
	if result.Operation.ID == "" {
		return errors.NewCode(errors.InvalidInput, "operation id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initializeLocked()
	if _, ok := s.deleted[result.Operation.ID]; ok {
		return errors.NewCode(errors.Conflict, "operation has been deleted").
			WithContext("operation_id", result.Operation.ID)
	}
	if existingID, ok := s.idempotencyKeys[key]; ok {
		if existing, exists := s.records[existingID]; exists {
			if existing.Operation.ID != result.Operation.ID {
				return errors.NewCode(errors.Conflict, "idempotency key already exists").
					WithContext("idempotency_key", key).
					WithContext("operation_id", existing.Operation.ID)
			}
		}
		if existingID != result.Operation.ID {
			s.removeIdempotencyKeyLocked(key, existingID)
		}
	}
	s.records[result.Operation.ID] = CloneResult(result)
	delete(s.idempotencyReservations, result.Operation.ID)
	s.trackRecordOrderLocked(result.Operation.ID)
	s.evictOverflowRecordsLocked(result.Operation.ID)
	// 防御性校验：绑定前确认目标记录仍在（当前淘汰实现保证 protectID 保位，
	// 此检查兜底未来实现变更）。
	if _, exists := s.records[result.Operation.ID]; !exists {
		return errors.NewCode(errors.Internal, "operation record was evicted during write").
			WithContext("operation_id", result.Operation.ID)
	}
	s.bindIdempotencyKeyLocked(key, result.Operation.ID)
	return nil
}

// ReserveIdempotencyKey 在业务执行前预占幂等键。
//
// 预占占位记录计入容量但不进入 FIFO 顺序表（预占不可被淘汰）；
// 由此造成的容量缺口会在预占释放后的写入中恢复。
func (s *MemoryStore) ReserveIdempotencyKey(ctx context.Context, key string, operationID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "memory operation store is nil")
	}
	if key == "" {
		return nil
	}
	if operationID == "" {
		return errors.NewCode(errors.InvalidInput, "operation id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initializeLocked()
	if _, ok := s.deleted[operationID]; ok {
		return errors.NewCode(errors.Conflict, "operation has been deleted").
			WithContext("operation_id", operationID)
	}
	if existingID, ok := s.idempotencyKeys[key]; ok {
		if existing, exists := s.records[existingID]; exists {
			if existing.Operation.ID == operationID {
				return nil
			}
			return errors.NewCode(errors.Conflict, "idempotency key already exists").
				WithContext("idempotency_key", key).
				WithContext("operation_id", existing.Operation.ID)
		}
		s.removeIdempotencyKeyLocked(key, existingID)
	}
	if _, exists := s.records[operationID]; exists {
		return errors.NewCode(errors.Conflict, "operation id is already in use").WithContext("operation_id", operationID)
	}
	s.records[operationID] = &Result{Operation: Operation{ID: operationID, Mode: ModeTracked, Status: StatusAccepted}}
	s.idempotencyReservations[operationID] = struct{}{}
	s.bindIdempotencyKeyLocked(key, operationID)
	return nil
}

// ReleaseIdempotencyKey 释放同一 operation 尚未完成的幂等键预占。
func (s *MemoryStore) ReleaseIdempotencyKey(ctx context.Context, key string, operationID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "memory operation store is nil")
	}
	if key == "" {
		return nil
	}
	if operationID == "" {
		return errors.NewCode(errors.InvalidInput, "operation id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initializeLocked()
	existingID, ok := s.idempotencyKeys[key]
	if !ok {
		return nil
	}
	if existingID != operationID {
		return errors.NewCode(errors.Conflict, "idempotency key is owned by another operation").
			WithContext("idempotency_key", key).
			WithContext("operation_id", existingID)
	}
	_, exists := s.records[existingID]
	if exists && !s.isIdempotencyReservationLocked(existingID) {
		return nil
	}
	s.removeIdempotencyKeyLocked(key, existingID)
	delete(s.records, existingID)
	delete(s.idempotencyReservations, existingID)
	// 预占占位通常不进入顺序表；此处防御性清理，避免任何历史路径留下残留顺序项。
	s.untrackRecordOrderLocked(existingID)
	return nil
}

func (s *MemoryStore) isIdempotencyReservationLocked(operationID string) bool {
	_, ok := s.idempotencyReservations[operationID]
	return ok
}

func (s *MemoryStore) bindIdempotencyKeyLocked(key string, operationID string) {
	if key == "" || operationID == "" {
		return
	}
	s.idempotencyKeys[key] = operationID
	if s.idempotencyKeysByOpID[operationID] == nil {
		s.idempotencyKeysByOpID[operationID] = make(map[string]struct{})
	}
	s.idempotencyKeysByOpID[operationID][key] = struct{}{}
}

func (s *MemoryStore) removeIdempotencyKeyLocked(key string, operationID string) {
	delete(s.idempotencyKeys, key)
	if operationID == "" {
		return
	}
	keys := s.idempotencyKeysByOpID[operationID]
	if keys == nil {
		return
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(s.idempotencyKeysByOpID, operationID)
	}
}
