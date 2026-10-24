package operation

import (
	"context"
	"sync"

	"gochen/errors"
)

// MemoryStore 提供轻量内存实现，适合短期、测试或单进程场景。
//
// 说明：Delete 会保留进程内 tombstone，阻止近期 in-flight 幂等调用把已删除 operation 复活；
// tombstone 有上限自动裁剪，也可通过 PurgeDeleted 手动清理；长期高频场景应使用持久化 store。
//
// 容量上限：默认不限制（保持既有行为）；通过 WithMaxRecords 配置后，
// 记录数超过上限时按写入顺序淘汰最旧的非预占记录（FIFO，非 LRU）。
// 被淘汰记录的幂等键绑定同步解除，后续同键请求视为首次执行；
// 预占中的记录（in-flight）与 tombstone 拦截范围内的记录不会被淘汰。
//
// 预占占位记录计入容量但不参与 FIFO 淘汰；当预占/tombstone 占据的容量
// 使可淘汰记录不足以回收溢出时，本调用刚写入的记录始终保位
// （绝不淘汰本次写入的记录），实际记录数可暂时超过 maxRecords，
// 待预占释放后恢复。这是有意取舍：宁可短暂超容量，也不静默丢失
// 刚报成功的写入（那会形成指向已删除记录的幽灵幂等绑定）。
type MemoryStore struct {
	mu                      sync.RWMutex
	records                 map[string]*Result
	idempotencyKeys         map[string]string
	idempotencyKeysByOpID   map[string]map[string]struct{}
	idempotencyReservations map[string]struct{}
	// deleted 保存 tombstone，阻止完成较晚的 owner 结果把已删除 operation 复活。
	deleted      map[string]struct{}
	deletedOrder []string
	// recordOrder 按 Put 顺序记录 operation id，用于容量上限的 FIFO 淘汰。
	// 仅在 maxRecords > 0 时维护；被 Delete 等路径移除的 id 以 "" 占位，
	// 由惰性压缩回收（recordOrderIndex 提供 O(1) 成员查询与移除）。
	recordOrder      []string
	recordOrderIndex map[string]int
	maxRecords       int
}

// NewMemoryStore 创建内存 operation store（无容量上限）。
func NewMemoryStore(options ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		records:                 make(map[string]*Result),
		idempotencyKeys:         make(map[string]string),
		idempotencyKeysByOpID:   make(map[string]map[string]struct{}),
		idempotencyReservations: make(map[string]struct{}),
		deleted:                 make(map[string]struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

// MemoryStoreOption 定制 MemoryStore 构造选项。
type MemoryStoreOption func(*MemoryStore)

// WithMaxRecords 设置记录容量上限（FIFO 淘汰最旧的非预占记录）。
//
// maxRecords <= 0 表示无上限。上限只约束 records/idempotency 映射；
// tombstone 上限独立生效（见 defaultMaxDeletedTombstones）。
// 必须在首次并发写入前配置（构造后立即链式调用）。
func WithMaxRecords(maxRecords int) MemoryStoreOption {
	return func(s *MemoryStore) {
		s.maxRecords = maxRecords
	}
}

// Get 按 operation id 读取 envelope。
func (s *MemoryStore) Get(ctx context.Context, id string) (*Result, error) {
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
	if err := contextError(ctx); err != nil {
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
	s.trackRecordOrderLocked(result.Operation.ID)
	// 本调用刚写入的记录必须保位：Put 报成功后 Get 必须能读到，
	// 且不能把幂等键淘汰成幽灵绑定。
	s.evictOverflowRecordsLocked(result.Operation.ID)
	return nil
}

// trackRecordOrderLocked 记录写入顺序（容量上限启用时），重复 Put 覆盖不改变顺序。
// 依靠 recordOrderIndex 做 O(1) 查重，避免每次 Put 对全表线性扫描。
func (s *MemoryStore) trackRecordOrderLocked(id string) {
	if s.maxRecords <= 0 {
		return
	}
	if _, ok := s.recordOrderIndex[id]; ok {
		return
	}
	s.recordOrderIndex[id] = len(s.recordOrder)
	s.recordOrder = append(s.recordOrder, id)
}

// untrackRecordOrderLocked 把 id 从 FIFO 顺序表中移除（O(1)）。
// 切片槽位以 "" 占位，惰性压缩回收，避免低于容量运行时无界累积。
func (s *MemoryStore) untrackRecordOrderLocked(id string) {
	if s.maxRecords <= 0 {
		return
	}
	idx, ok := s.recordOrderIndex[id]
	if !ok {
		return
	}
	delete(s.recordOrderIndex, id)
	if idx >= 0 && idx < len(s.recordOrder) && s.recordOrder[idx] == id {
		s.recordOrder[idx] = ""
	}
	s.maybeCompactRecordOrderLocked()
}

// maybeCompactRecordOrderLocked 在占位项占比过高时压缩 recordOrder 并重建索引。
func (s *MemoryStore) maybeCompactRecordOrderLocked() {
	stale := len(s.recordOrder) - len(s.recordOrderIndex)
	if stale < 64 || stale*2 <= len(s.recordOrder) {
		return
	}
	kept := s.recordOrder[:0]
	for _, id := range s.recordOrder {
		if id == "" {
			continue
		}
		kept = append(kept, id)
	}
	s.recordOrder = kept
	for i, id := range s.recordOrder {
		s.recordOrderIndex[id] = i
	}
	if len(s.recordOrder) == 0 {
		s.recordOrder = nil
	}
}

// evictOverflowRecordsLocked 超出容量上限时按 FIFO 淘汰最旧的非预占记录。
// 淘汰会同步解除该记录的幂等键绑定；tombstone 拦截范围内的记录不淘汰。
// protectID 是本次调用刚写入/补存的记录，永不参与本次淘汰——
// 溢出无法回收时宁可暂时超容量，也不静默丢弃刚报成功的写入。
func (s *MemoryStore) evictOverflowRecordsLocked(protectID string) {
	if s.maxRecords <= 0 || len(s.records) <= s.maxRecords {
		return
	}
	overflow := len(s.records) - s.maxRecords
	kept := s.recordOrder[:0]
	for _, id := range s.recordOrder {
		if id == "" {
			// 已被 Delete 等路径移除的惰性占位，顺手清理。
			continue
		}
		if _, ok := s.records[id]; !ok {
			// 残留顺序项：仅清理顺序表，不消耗溢出配额。
			delete(s.recordOrderIndex, id)
			continue
		}
		if overflow <= 0 {
			kept = append(kept, id)
			continue
		}
		if id == protectID || s.isIdempotencyReservationLocked(id) {
			kept = append(kept, id)
			continue
		}
		if _, ok := s.deleted[id]; ok {
			kept = append(kept, id)
			continue
		}
		for key := range s.idempotencyKeysByOpID[id] {
			s.removeIdempotencyKeyLocked(key, id)
		}
		delete(s.idempotencyKeysByOpID, id)
		delete(s.records, id)
		delete(s.recordOrderIndex, id)
		overflow--
	}
	s.recordOrder = kept
	for i, id := range s.recordOrder {
		s.recordOrderIndex[id] = i
	}
	if len(s.recordOrder) == 0 {
		s.recordOrder = nil
	}
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
	if s.recordOrderIndex == nil {
		s.recordOrderIndex = make(map[string]int)
	}
}

var _ IIdempotencyStore = (*MemoryStore)(nil)

var _ IIdempotencyReservationStore = (*MemoryStore)(nil)

var _ IIdempotencyReservationReleaseStore = (*MemoryStore)(nil)
