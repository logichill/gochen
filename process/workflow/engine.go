package workflow

import (
	"context"
	"strings"
	"sync"
	"time"

	"gochen/clock"
	"gochen/errors"
)

// Engine 基于显式定义与实例状态推进工作流。
type Engine struct {
	store IStore
	clk   clock.IClock

	// instanceLocks 在进程内串行化同一个实例的状态推进，避免并发读改写互相覆盖。
	instanceMu    sync.Mutex
	instanceLocks map[ID]*instanceLock
}

type instanceLock struct {
	mu   sync.Mutex
	refs int
}

// NewEngine 创建工作流引擎。
func NewEngine(store IStore) *Engine {
	return (&Engine{store: store}).WithClock(clock.NewRealClock())
}

// WithClock 为引擎注入时钟。
func (e *Engine) WithClock(clk clock.IClock) *Engine {
	if e == nil {
		return e
	}
	if clk != nil {
		e.clk = clk
	}
	return e
}

// SaveDefinition 保存或更新工作流定义。
func (e *Engine) SaveDefinition(ctx context.Context, def *Definition) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := validateDefinition(def); err != nil {
		return err
	}

	now := e.now()
	cp := cloneDefinition(def)
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	return e.store.SaveDefinition(ctx, cp)
}

// CreateInstance 创建一个待启动的工作流实例。
func (e *Engine) CreateInstance(ctx context.Context, instanceID ID, definitionID string) error {
	return e.createInstance(ctx, instanceID, definitionID, nil)
}

// CreateInstanceWithData 创建一个带初始业务数据的待启动工作流实例。
func (e *Engine) CreateInstanceWithData(ctx context.Context, instanceID ID, definitionID string, data map[string]any) error {
	return e.createInstance(ctx, instanceID, definitionID, data)
}

func (e *Engine) createInstance(ctx context.Context, instanceID ID, definitionID string, initialData map[string]any) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(instanceID)) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	def, err := e.loadDefinition(ctx, definitionID)
	if err != nil {
		return err
	}
	if def == nil {
		return errors.NewCode(errors.NotFound, "workflow definition not found").
			WithContext("definition_id", definitionID)
	}

	now := e.now()
	st := &State{
		ID:               instanceID,
		DefinitionID:     def.ID,
		Status:           InstanceStatusPending,
		ActiveNodeIDs:    []string{},
		PendingJoins:     []PendingJoin{},
		CompletedNodeIDs: []string{},
		History: []HistoryEntry{
			{Action: "create", At: now},
		},
		Data:      initialInstanceData(initialData),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, ok := e.store.(IOptimisticStore); ok {
		return e.saveInstance(ctx, st, 0)
	}

	existing, err := e.store.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.NewCode(errors.Conflict, "workflow instance already exists").
			WithContext("instance_id", string(instanceID))
	}
	return e.saveInstance(ctx, st, 0)
}

// StartInstance 启动一个待执行的工作流实例。
func (e *Engine) StartInstance(ctx context.Context, instanceID ID) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, err := e.loadInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if st == nil {
		return errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(instanceID))
	}
	if st.Status != InstanceStatusPending {
		return errors.NewCode(errors.Conflict, "workflow instance is not pending").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	def, err := e.loadDefinition(ctx, st.DefinitionID)
	if err != nil {
		return err
	}
	if def == nil {
		return errors.NewCode(errors.NotFound, "workflow definition not found").
			WithContext("definition_id", st.DefinitionID).
			WithContext("instance_id", string(instanceID))
	}

	now := e.now()
	st.Status = InstanceStatusRunning
	st.StartedAt = now
	st.UpdatedAt = now
	st.ActiveNodeIDs = []string{def.StartNodeID}
	st.PendingJoins = []PendingJoin{}
	st.CurrentNodeID = def.StartNodeID
	st.History = append(st.History, HistoryEntry{
		Action: "start",
		NodeID: def.StartNodeID,
		At:     now,
	})
	return e.saveInstance(ctx, st, st.Version)
}

// Advance 完成当前唯一活动节点并推进流程。
func (e *Engine) Advance(ctx context.Context, instanceID ID) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if len(st.ActiveNodeIDs) != 1 {
		return errors.NewCode(errors.Conflict, "workflow instance requires explicit node selection").
			WithContext("instance_id", string(instanceID)).
			WithContext("active_nodes", len(st.ActiveNodeIDs))
	}
	if err := e.advanceNode(st, def, st.ActiveNodeIDs[0], e.now()); err != nil {
		return err.WithContext("instance_id", string(instanceID))
	}
	return e.saveInstance(ctx, st, st.Version)
}

// AdvanceNode 完成指定活动节点并继续推进工作流。
func (e *Engine) AdvanceNode(ctx context.Context, instanceID ID, nodeID string) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := e.advanceNode(st, def, nodeID, e.now()); err != nil {
		return err.WithContext("instance_id", string(instanceID))
	}
	return e.saveInstance(ctx, st, st.Version)
}

// AdvanceNodeTo 完成指定活动节点，并只激活给定的单个后继节点。
//
// 该方法用于选择网关或业务状态机：调用方显式决定下一步，而不是默认激活全部出边。
// 显式选择只影响从当前节点走哪条出边；目标节点仍遵守原有的 join 等待语义。
//
// 注意：调用方需确保被跳过的分支不会导致下游 join 节点永远无法满足。
func (e *Engine) AdvanceNodeTo(ctx context.Context, instanceID ID, nodeID string, nextNodeID string) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}
	if strings.TrimSpace(nextNodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow next node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := e.advanceNodeTo(st, def, nodeID, &nextNodeID, e.now()); err != nil {
		return err.WithContext("instance_id", string(instanceID))
	}
	return e.saveInstance(ctx, st, st.Version)
}

// updateInstanceData 对已存在实例的 Data 字段执行受限更新。
func (e *Engine) updateInstanceData(ctx context.Context, instance ID, update func(data map[string]any) error) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if update == nil {
		return errors.NewCode(errors.InvalidInput, "update function cannot be nil")
	}

	unlock := e.lockInstance(instance)
	defer unlock()

	st, err := e.loadInstance(ctx, instance)
	if err != nil {
		return err
	}
	if st == nil {
		return errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(instance))
	}
	if st.Data == nil {
		st.Data = map[string]any{}
	}
	if err := update(st.Data); err != nil {
		return err
	}
	st.UpdatedAt = e.now()
	return e.saveInstance(ctx, st, st.Version)
}

func initialInstanceData(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return cloneWorkflowMap(value)
}

// validateStore 确认引擎和底层存储已经初始化。
func (e *Engine) validateStore() error {
	if e == nil || e.store == nil {
		return errors.NewCode(errors.InvalidInput, "workflow engine store is nil")
	}
	return nil
}

// saveInstance 优先使用乐观锁存储；普通存储则只维护本地版本初值。
func (e *Engine) saveInstance(ctx context.Context, st *State, expectedVersion uint64) error {
	if optimistic, ok := e.store.(IOptimisticStore); ok {
		return optimistic.SaveIfVersion(ctx, st, expectedVersion)
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return e.store.Save(ctx, st)
}

// validateContext 拒绝 nil context，避免存储实现遇到不可预期输入。
func validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "workflow context is nil")
	}
	return nil
}

// lockInstance 获取实例级互斥锁，并在释放后清理无人使用的锁对象。
func (e *Engine) lockInstance(instanceID ID) func() {
	e.instanceMu.Lock()
	if e.instanceLocks == nil {
		e.instanceLocks = map[ID]*instanceLock{}
	}
	lock := e.instanceLocks[instanceID]
	if lock == nil {
		lock = &instanceLock{}
		e.instanceLocks[instanceID] = lock
	}
	lock.refs++
	e.instanceMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()

		e.instanceMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(e.instanceLocks, instanceID)
		}
		e.instanceMu.Unlock()
	}
}

// now 返回当前时钟时间；未显式注入时钟时使用真实时钟。
func (e *Engine) now() time.Time {
	if e.clk == nil {
		e.clk = clock.NewRealClock()
	}
	return e.clk.Now()
}

// loadDefinition 读取并重新校验流程定义，防止存储中存在非法图结构。
func (e *Engine) loadDefinition(ctx context.Context, definitionID string) (*Definition, error) {
	if strings.TrimSpace(definitionID) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "workflow definition id is empty")
	}

	def, err := e.store.GetDefinition(ctx, definitionID)
	if err != nil || def == nil {
		return def, err
	}
	if err := validateDefinition(def); err != nil {
		return nil, err.WithContext("definition_id", def.ID)
	}
	return def, nil
}

// loadInstance 读取实例状态，并统一校验实例 ID。
func (e *Engine) loadInstance(ctx context.Context, instanceID ID) (*State, error) {
	if strings.TrimSpace(string(instanceID)) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}
	return e.store.Get(ctx, instanceID)
}

// loadRunningInstance 读取运行中实例及其定义，供推进类操作复用。
func (e *Engine) loadRunningInstance(ctx context.Context, instanceID ID) (*State, *Definition, error) {
	st, err := e.loadInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(instanceID))
	}
	if st.Status != InstanceStatusRunning {
		return nil, nil, errors.NewCode(errors.Conflict, "workflow instance is not running").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	def, err := e.loadDefinition(ctx, st.DefinitionID)
	if err != nil {
		return nil, nil, err
	}
	if def == nil {
		return nil, nil, errors.NewCode(errors.NotFound, "workflow definition not found").
			WithContext("definition_id", st.DefinitionID).
			WithContext("instance_id", string(instanceID))
	}
	return st, def, nil
}
