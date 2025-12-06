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
	Data      map[string]any
	UpdatedAt time.Time
}

// IStore 流程状态存储接口（最小化）
type IStore interface {
	Get(ctx context.Context, id ID) (*State, error)
	Save(ctx context.Context, st *State) error
	Delete(ctx context.Context, id ID) error
}

// Manager 进程管理器（最小骨架）
// 目标：为 Saga/流程编排提供最少必要抽象，避免框架耦合。
//
// # 并发模型与线程安全
//
// ⚠️ 同一个流程实例在并发场景下不是 goroutine 安全的：
//   - 对同一实例 ID 并发调用 HandleCommand() 会导致更新丢失（典型的读-改-写 TOCTOU 问题）；
//   - 当前实现中没有内部锁，也没有乐观锁控制。
//
// 安全的使用方式：
//   - ✅ 不同实例 ID 的流程可以并发处理；
//   - ✅ 同一实例 ID 的 HandleCommand() 调用必须串行；
//   - ❌ 不要对同一实例 ID 并发调用 HandleCommand()。
//
// 在生产环境中，如需对同一实例进行并发命令处理，可以考虑：
//   - 在 IStore 实现中引入版本号 + CAS 的乐观锁；
//   - 为每个实例 ID 引入外部分布式锁；
//   - 通过工作队列将同一实例的命令串行化。
//
// # 可选增强：乐观锁存储接口
//
// 如果希望在框架层统一抽象乐观锁，可以考虑定义如下接口：
//
//	type IOptimisticStore interface {
//	    // Get 获取状态及其版本号
//	    Get(ctx context.Context, id ID) (*State, uint64, error)
//
//	    // SaveWithVersion 在版本匹配时保存状态（CAS）
//	    // 若版本不匹配返回 ErrVersionMismatch
//	    SaveWithVersion(ctx context.Context, st *State, expectedVersion uint64) error
//	}
//
// 可能的实现方式：
//   - 基于 SQL：使用 version 列并在 UPDATE 时带上 WHERE version = ? 条件；
//   - 基于 NoSQL：使用条件写入（例如 DynamoDB、Cosmos DB 等）；
//   - 基于内存：使用原子 CAS 操作维护版本。
//
// 集成点：可将 HandleCommand 改写为基于版本的读取与保存逻辑。
type Manager struct {
	store IStore
}

func NewManager(store IStore) *Manager { return &Manager{store: store} }

// HandleCommand 处理命令触发（占位）
func (m *Manager) HandleCommand(ctx context.Context, instance ID, update func(st *State) error) error {
	st, _ := m.store.Get(ctx, instance)
	if st == nil {
		st = &State{ID: instance, Data: map[string]any{}, UpdatedAt: time.Now()}
	}
	if err := update(st); err != nil {
		return err
	}
	st.UpdatedAt = time.Now()
	return m.store.Save(ctx, st)
}

// Timeout/Compensate 等能力可以按需扩展（此处提供占位定义，避免过度设计）
