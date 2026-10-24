package workflow

import (
	"sync"
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

var _ IOptimisticStore = (*MemoryStore)(nil)

var _ IQueryableStore = (*MemoryStore)(nil)
