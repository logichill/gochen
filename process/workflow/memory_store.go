package workflow

import (
	"context"
	"sort"
	"sync"
	"time"

	"gochen/errors"
)

// MemoryStore 是用于测试与轻量单机场景的内存存储实现。
// 实现了 IOptimisticStore 接口，并支持流程定义的多版本管理。
type MemoryStore struct {
	mu             sync.RWMutex
	definitions    map[string]map[uint32]*Definition
	latestVersions map[string]uint32
	states         map[ID]*State
}

// NewMemoryStore 创建一个新的内存工作流存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		definitions:    map[string]map[uint32]*Definition{},
		latestVersions: map[string]uint32{},
		states:         map[ID]*State{},
	}
}

// GetDefinition 查询指定版本的流程定义；version == 0 时返回最新版本。
func (s *MemoryStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	verMap, exists := s.definitions[id]
	if !exists || len(verMap) == 0 {
		return nil, nil
	}

	targetVersion := version
	if targetVersion == 0 {
		targetVersion = s.latestVersions[id]
	}

	def, exists := verMap[targetVersion]
	if !exists || def == nil {
		return nil, nil
	}
	return cloneDefinition(def), nil
}

// SaveDefinition 保存或覆盖流程定义。若 def.Version 为 0，自动分配最新版本号。
func (s *MemoryStore) SaveDefinition(ctx context.Context, def *Definition) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if def == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.definitions[def.ID] == nil {
		s.definitions[def.ID] = map[uint32]*Definition{}
	}

	if def.Version == 0 {
		latest := s.latestVersions[def.ID]
		def.Version = latest + 1
	}

	s.definitions[def.ID][def.Version] = cloneDefinition(def)
	if def.Version > s.latestVersions[def.ID] {
		s.latestVersions[def.ID] = def.Version
	}
	return nil
}

// DeleteDefinition 删除指定版本（version == 0 删除所有版本）的流程定义。
func (s *MemoryStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if version == 0 {
		delete(s.definitions, id)
		delete(s.latestVersions, id)
		return nil
	}

	verMap, exists := s.definitions[id]
	if exists {
		delete(verMap, version)
		if len(verMap) == 0 {
			delete(s.definitions, id)
			delete(s.latestVersions, id)
		} else {
			// 重新计算最新版本
			var maxVer uint32
			for v := range verMap {
				if v > maxVer {
					maxVer = v
				}
			}
			s.latestVersions[id] = maxVer
		}
	}
	return nil
}

// Get 查询流程实例状态。
func (s *MemoryStore) Get(ctx context.Context, id ID) (*State, error) {
	if s == nil {
		return nil, errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[id]
	if !exists || st == nil {
		return nil, nil
	}
	return cloneState(st), nil
}

// Save 保存或覆盖流程实例状态。
func (s *MemoryStore) Save(ctx context.Context, st *State) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if st == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[st.ID] = cloneState(st)
	return nil
}

// SaveIfVersion 按乐观并发版本号保存流程实例状态。
func (s *MemoryStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	if st == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.states[st.ID]
	if expectedVersion == 0 {
		if exists && current != nil {
			return errors.NewCode(errors.Conflict, "workflow instance already exists").
				WithContext("instance_id", string(st.ID))
		}
		st.Version = 1
		s.states[st.ID] = cloneState(st)
		return nil
	}

	if !exists || current == nil {
		return errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(st.ID))
	}
	if current.Version != expectedVersion {
		return errors.NewCode(errors.Conflict, "workflow instance version conflict").
			WithContext("instance_id", string(st.ID)).
			WithContext("expected_version", expectedVersion).
			WithContext("actual_version", current.Version)
	}

	st.Version = expectedVersion + 1
	s.states[st.ID] = cloneState(st)
	return nil
}

// Delete 删除流程实例状态。
func (s *MemoryStore) Delete(ctx context.Context, id ID) error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "workflow store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, id)
	return nil
}

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
	sort.Slice(matched, func(i, j int) bool {
		left, right := oldestActiveAt(matched[i]), oldestActiveAt(matched[j])
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

var _ IOptimisticStore = (*MemoryStore)(nil)
var _ IQueryableStore = (*MemoryStore)(nil)
