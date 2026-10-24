package workflow

import (
	"context"

	"gochen/errors"
)

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
