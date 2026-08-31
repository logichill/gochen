package operation

import (
	"context"
	"sync"

	"gochen/errors"
	"gochen/internal/clonevalue"
)

const defaultMaxDeletedTombstones = 4096

// IStore 抽象 operation envelope 的最小持久化能力。
type IStore interface {
	// Get 按 operation id 读取结果；未命中必须返回可被 IsStoreNotFound 识别的错误。
	Get(ctx context.Context, id string) (*Result, error)
	Put(ctx context.Context, result *Result) error
	Delete(ctx context.Context, id string) error
}

// IIdempotencyStore 可选扩展：按业务幂等键复用 tracked operation 结果。
//
// GetByIdempotencyKey 未命中必须返回可被 IsStoreNotFound 识别的错误。
//
// PutWithIdempotencyKey 必须以原子方式同时保存 result 并绑定 key：
// - key 已绑定到仍存在的 operation 时返回 Conflict，不得覆盖；
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

// MemoryStore 提供轻量内存实现，适合短期、测试或单进程场景。
//
// 说明：Delete 会保留进程内 tombstone，阻止近期 in-flight 幂等调用把已删除 operation 复活；
// tombstone 有上限自动裁剪，也可通过 PurgeDeleted 手动清理；长期高频场景应使用持久化 store。
type MemoryStore struct {
	mu                      sync.RWMutex
	records                 map[string]*Result
	idempotencyKeys         map[string]string
	idempotencyKeysByOpID   map[string]map[string]struct{}
	idempotencyReservations map[string]struct{}
	// deleted 保存 tombstone，阻止完成较晚的 owner 结果把已删除 operation 复活。
	deleted      map[string]struct{}
	deletedOrder []string
}

// NewMemoryStore 创建内存 operation store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records:                 make(map[string]*Result),
		idempotencyKeys:         make(map[string]string),
		idempotencyKeysByOpID:   make(map[string]map[string]struct{}),
		idempotencyReservations: make(map[string]struct{}),
		deleted:                 make(map[string]struct{}),
	}
}

// Get 按 operation id 读取 envelope。
func (s *MemoryStore) Get(ctx context.Context, id string) (*Result, error) {
	if err := memoryStoreContextError(ctx); err != nil {
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

	result, ok := s.records[id]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	if s.isIdempotencyReservationLocked(id) {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	return CloneResult(result), nil
}

// GetByIdempotencyKey 按幂等键读取已有 tracked operation 结果。
func (s *MemoryStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	if err := memoryStoreContextError(ctx); err != nil {
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

// Put 保存或覆盖 operation envelope。
func (s *MemoryStore) Put(ctx context.Context, result *Result) error {
	if err := memoryStoreContextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "memory operation store is nil")
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
	s.records[result.Operation.ID] = CloneResult(result)
	delete(s.idempotencyReservations, result.Operation.ID)
	return nil
}

// PutWithIdempotencyKey 原子保存 operation 并把幂等键绑定到该 operation。
func (s *MemoryStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if err := memoryStoreContextError(ctx); err != nil {
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
	s.bindIdempotencyKeyLocked(key, result.Operation.ID)
	return nil
}

// ReserveIdempotencyKey 在业务执行前预占幂等键。
func (s *MemoryStore) ReserveIdempotencyKey(ctx context.Context, key string, operationID string) error {
	if err := memoryStoreContextError(ctx); err != nil {
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
	s.records[operationID] = &Result{Operation: Operation{ID: operationID, Mode: ModeTracked, Status: StatusAccepted}}
	s.idempotencyReservations[operationID] = struct{}{}
	s.bindIdempotencyKeyLocked(key, operationID)
	return nil
}

// ReleaseIdempotencyKey 释放同一 operation 尚未完成的幂等键预占。
func (s *MemoryStore) ReleaseIdempotencyKey(ctx context.Context, key string, operationID string) error {
	if err := memoryStoreContextError(ctx); err != nil {
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

// Delete 删除 operation envelope。
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := memoryStoreContextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "memory operation store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initializeLocked()
	if id != "" {
		s.addDeletedTombstoneLocked(id)
	}
	for key := range s.idempotencyKeysByOpID[id] {
		s.removeIdempotencyKeyLocked(key, id)
	}
	delete(s.idempotencyKeysByOpID, id)
	delete(s.records, id)
	delete(s.idempotencyReservations, id)
	s.pruneDeletedTombstonesLocked()
	return nil
}

func (s *MemoryStore) addDeletedTombstoneLocked(id string) {
	if _, ok := s.deleted[id]; ok {
		return
	}
	s.deleted[id] = struct{}{}
	s.deletedOrder = append(s.deletedOrder, id)
}

func (s *MemoryStore) pruneDeletedTombstonesLocked() {
	for len(s.deleted) > defaultMaxDeletedTombstones && len(s.deletedOrder) > 0 {
		id := s.deletedOrder[0]
		s.deletedOrder = s.deletedOrder[1:]
		if _, ok := s.deleted[id]; ok {
			delete(s.deleted, id)
		}
	}
	if len(s.deleted) == 0 {
		s.deletedOrder = nil
		return
	}
	if len(s.deletedOrder) > 2*defaultMaxDeletedTombstones || cap(s.deletedOrder) > 2*defaultMaxDeletedTombstones {
		s.compactDeletedOrderLocked()
	}
}

func (s *MemoryStore) compactDeletedOrderLocked() {
	if len(s.deleted) == 0 {
		s.deletedOrder = nil
		return
	}
	seen := make(map[string]struct{}, len(s.deleted))
	order := make([]string, 0, len(s.deleted))
	for _, id := range s.deletedOrder {
		if _, ok := s.deleted[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		order = append(order, id)
	}
	s.deletedOrder = order
}

// PurgeDeleted 清理 Delete 留下的 tombstone，并返回实际清理数量。
//
// 说明：MemoryStore 的 tombstone 用于阻止近期 in-flight 幂等调用复活已删除 operation；
// 长期高频 Delete 场景可在确认相关 in-flight 调用均已结束后调用本方法回收内存。
// 不传 id 时清理全部 tombstone。
func (s *MemoryStore) PurgeDeleted(ids ...string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializeLocked()

	if len(ids) == 0 {
		n := len(s.deleted)
		s.deleted = make(map[string]struct{})
		s.deletedOrder = nil
		return n
	}
	n := 0
	for _, id := range ids {
		if _, ok := s.deleted[id]; ok {
			delete(s.deleted, id)
			n++
		}
	}
	if len(s.deleted) == 0 {
		s.deletedOrder = nil
	} else {
		s.compactDeletedOrderLocked()
	}
	return n
}

func (s *MemoryStore) initializeLocked() {
	if s.records == nil {
		s.records = make(map[string]*Result)
	}
	if s.idempotencyKeys == nil {
		s.idempotencyKeys = make(map[string]string)
	}
	if s.idempotencyKeysByOpID == nil {
		s.idempotencyKeysByOpID = make(map[string]map[string]struct{})
	}
	if s.idempotencyReservations == nil {
		s.idempotencyReservations = make(map[string]struct{})
	}
	if s.deleted == nil {
		s.deleted = make(map[string]struct{})
	}
}

func memoryStoreContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

var _ IIdempotencyStore = (*MemoryStore)(nil)
var _ IIdempotencyReservationStore = (*MemoryStore)(nil)
var _ IIdempotencyReservationReleaseStore = (*MemoryStore)(nil)

// CloneResult 深拷贝 operation result，避免调用方共享内部可变引用。
func CloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}

	cloned := *result
	if result.Resource != nil {
		resource := *result.Resource
		cloned.Resource = &resource
	}
	if result.Error != nil {
		errCopy := *result.Error
		if result.Error.Details != nil {
			errCopy.Details = clonevalue.MapStringAny(result.Error.Details)
		}
		cloned.Error = &errCopy
	}
	if result.Result != nil {
		cloned.Result = clonevalue.MapStringAny(result.Result)
	}
	if result.AffectedScopes != nil {
		cloned.AffectedScopes = append([]string(nil), result.AffectedScopes...)
	}
	return &cloned
}
