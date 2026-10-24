package operation

import (
	"context"

	"gochen/errors"
)

const defaultMaxDeletedTombstones = 4096

// Delete 删除 operation envelope。
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
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
		// 即时清理 FIFO 顺序项：否则低于容量运行时（淘汰永不触发），
		// 顺序表会随 put/delete 流水无界累积历史 id。
		s.untrackRecordOrderLocked(id)
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
		delete(s.deleted, id)
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
