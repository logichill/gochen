package workflow

import (
	"context"
	"sort"
	"time"

	"gochen/errors"
)

// ListInstances 按查询条件返回实例状态列表，按最早活动时刻升序排序。
func (s *MemoryStore) ListInstances(ctx context.Context, query InstanceQuery) ([]*State, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if query.Limit <= 0 {
		return nil, errors.NewCode(errors.InvalidInput, "workflow instance query limit must be positive")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]*State, 0, len(s.states))
	for _, st := range s.states {
		if st == nil || !matchesInstanceQuery(st, query) {
			continue
		}
		matched = append(matched, st)
	}

	// 最早活动的实例排在前面，使有界扫描优先命中最可能超时的实例；
	// 活动时刻相同时按 ID 排序，保证结果稳定可分页。
	// 无活动节点的实例（零值时间）不可能是超时候选，必须排在末尾，
	// 否则零值会被判定早于任意真实时间戳，会永久占据扫描窗口前列，
	// 把真正超时的实例饿死。
	sort.Slice(matched, func(i, j int) bool {
		left, right := oldestActiveAt(matched[i]), oldestActiveAt(matched[j])
		if left.IsZero() || right.IsZero() {
			if left.IsZero() && right.IsZero() {
				return matched[i].ID < matched[j].ID
			}
			return right.IsZero()
		}
		if !left.Equal(right) {
			return left.Before(right)
		}
		return matched[i].ID < matched[j].ID
	})

	if len(matched) > query.Limit {
		matched = matched[:query.Limit]
	}
	out := make([]*State, 0, len(matched))
	for _, st := range matched {
		out = append(out, cloneState(st))
	}
	return out, nil
}

func matchesInstanceQuery(st *State, query InstanceQuery) bool {
	if query.DefinitionID != "" && st.DefinitionID != query.DefinitionID {
		return false
	}
	if len(query.Statuses) > 0 {
		hit := false
		for _, status := range query.Statuses {
			if st.Status == status {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if !query.ActiveSince.IsZero() {
		oldest := oldestActiveAt(st)
		if oldest.IsZero() || oldest.After(query.ActiveSince) {
			return false
		}
	}
	return true
}

// oldestActiveAt 返回实例中最早的活动节点激活时刻；没有活动节点时返回零值。
func oldestActiveAt(st *State) time.Time {
	var oldest time.Time
	for _, nodeID := range st.ActiveNodeIDs {
		startedAt, ok := st.ActiveNodeStartedAt[nodeID]
		if !ok || startedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || startedAt.Before(oldest) {
			oldest = startedAt
		}
	}
	return oldest
}
