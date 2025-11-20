package process

import (
	"context"
	"time"
)

// ID 表示流程实例标识
type ID string

// State 流程状态最小模型
type State struct {
	ID        ID
	Data      map[string]interface{}
	UpdatedAt time.Time
}

// Store 流程状态存储接口（最小化）
type Store interface {
	Get(ctx context.Context, id ID) (*State, error)
	Save(ctx context.Context, st *State) error
	Delete(ctx context.Context, id ID) error
}

// Manager 进程管理器（最小骨架）
// 目标：为 Saga/流程编排提供最少必要抽象，避免框架耦合
type Manager struct {
	store Store
}

func NewManager(store Store) *Manager { return &Manager{store: store} }

// HandleCommand 处理命令触发（占位）
func (m *Manager) HandleCommand(ctx context.Context, instance ID, update func(st *State) error) error {
	st, _ := m.store.Get(ctx, instance)
	if st == nil {
		st = &State{ID: instance, Data: map[string]interface{}{}, UpdatedAt: time.Now()}
	}
	if err := update(st); err != nil {
		return err
	}
	st.UpdatedAt = time.Now()
	return m.store.Save(ctx, st)
}

// Timeout/Compensate 等能力可以按需扩展（此处提供占位定义，避免过度设计）
