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
	store        IStore
	clkMu        sync.RWMutex
	clk          clock.IClock
	evaluator    IConditionEvaluator
	interceptors []Interceptor
	nodeHooks    nodeHookRegistry

	// definitionLocks 在进程内串行化同一个 definition 的保存，避免并发更新覆盖 CreatedAt 与版本错乱。
	definitionMu    sync.Mutex
	definitionLocks map[string]*keyedLock[string]

	// instanceLocks 在进程内串行化同一个实例的状态推进，避免并发读改写互相覆盖。
	instanceMu    sync.Mutex
	instanceLocks map[ID]*keyedLock[ID]
}

type keyedLock[K comparable] struct {
	mu   sync.Mutex
	refs int
}

// StateMutation 允许调用方在引擎完成状态迁移后、保存实例前同步补充同一个 State。
// 回调在实例锁持有期间执行，应仅做轻量同步计算，不得包含 IO 或耗时操作。
type StateMutation func(st *State, now time.Time) error

// CreateOption 定义创建实例时的可选配置。
type CreateOption func(*createOptions)

type createOptions struct {
	version uint32
}

// WithDefinitionVersion 指定创建实例时绑定的流程定义版本号。若未指定或为 0 则默认绑定最新版本。
func WithDefinitionVersion(version uint32) CreateOption {
	return func(o *createOptions) {
		o.version = version
	}
}

// NewEngine 创建工作流引擎。
func NewEngine(store IStore) *Engine {
	return (&Engine{
		store:           store,
		evaluator:       NewDefaultConditionEvaluator(),
		definitionLocks: map[string]*keyedLock[string]{},
		instanceLocks:   map[ID]*keyedLock[ID]{},
	}).WithClock(clock.NewRealClock())
}

// WithClock 为引擎注入时钟；可在引擎运行期间调用，内部会同步后续 now 读取。
func (e *Engine) WithClock(clk clock.IClock) *Engine {
	if e == nil {
		return e
	}
	if clk != nil {
		e.clkMu.Lock()
		e.clk = clk
		e.clkMu.Unlock()
	}
	return e
}

// WithEvaluator 注入自定义条件表达式求值器。
func (e *Engine) WithEvaluator(evaluator IConditionEvaluator) *Engine {
	if e == nil {
		return e
	}
	if evaluator != nil {
		e.evaluator = evaluator
	}
	return e
}

// Use 注册全局工作流拦截器。
//
// 必须在引擎开始处理实例前完成注册；注册本身不是并发安全的。
func (e *Engine) Use(interceptors ...Interceptor) *Engine {
	if e == nil {
		return e
	}
	e.interceptors = append(e.interceptors, interceptors...)
	return e
}

// SaveDefinition 保存或更新工作流定义。若 def.Version 为 0，自动分配递增版本号。
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
	if err := e.validateDefinitionConditions(def); err != nil {
		return err
	}

	unlock := e.lockDefinition(def.ID)
	defer unlock()

	now := e.now()
	cp := cloneDefinition(def)

	var latest *Definition
	var err error
	needsLatest := cp.Version == 0 || cp.CreatedAt.IsZero()

	if needsLatest {
		latest, err = e.store.GetDefinition(ctx, cp.ID, 0)
		if err != nil && !errors.Is(err, errors.NotFound) {
			return err
		}
	}

	if cp.Version == 0 {
		if latest != nil && latest.Version > 0 {
			cp.Version = latest.Version + 1
		} else {
			cp.Version = 1
		}
	}

	if cp.CreatedAt.IsZero() {
		if latest != nil && !latest.CreatedAt.IsZero() {
			cp.CreatedAt = latest.CreatedAt
		} else {
			cp.CreatedAt = now
		}
	}
	cp.UpdatedAt = now
	if err := e.store.SaveDefinition(ctx, cp); err != nil {
		return err
	}
	def.Version = cp.Version
	def.CreatedAt = cp.CreatedAt
	def.UpdatedAt = cp.UpdatedAt
	return nil
}

// CreateInstance 创建一个待启动的工作流实例。
func (e *Engine) CreateInstance(ctx context.Context, instanceID ID, definitionID string, opts ...CreateOption) error {
	return e.createInstance(ctx, instanceID, definitionID, nil, opts...)
}

// CreateInstanceWithData 创建一个带初始业务数据的待启动工作流实例。
func (e *Engine) CreateInstanceWithData(ctx context.Context, instanceID ID, definitionID string, data map[string]any, opts ...CreateOption) error {
	return e.createInstance(ctx, instanceID, definitionID, data, opts...)
}

func (e *Engine) createInstance(ctx context.Context, instanceID ID, definitionID string, initialData map[string]any, opts ...CreateOption) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(instanceID)) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}

	var opt createOptions
	for _, o := range opts {
		if o != nil {
			o(&opt)
		}
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	def, err := e.loadDefinition(ctx, definitionID, opt.version)
	if err != nil {
		return err
	}
	if def == nil {
		return errors.NewCode(errors.NotFound, "workflow definition not found").
			WithContext("definition_id", definitionID).
			WithContext("definition_version", opt.version)
	}

	tctx := &TransitionContext{
		Action:            ActionCreate,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		Data:              cloneWorkflowMap(initialData),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		st := &State{
			ID:                  instanceID,
			DefinitionID:        def.ID,
			DefinitionVersion:   def.Version,
			Status:              InstanceStatusPending,
			ActiveNodeIDs:       []string{},
			ActiveNodeStartedAt: map[string]time.Time{},
			PendingJoins:        []PendingJoin{},
			CompletedNodeIDs:    []string{},
			History: []HistoryEntry{
				{Action: "create", At: now},
			},
			Data:      initialInstanceData(initialData),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, ok := e.store.(IOptimisticStore); ok {
			return e.saveInstance(c, st, 0)
		}

		existing, err := e.store.Get(c, instanceID)
		if err != nil {
			return err
		}
		if existing != nil {
			return errors.NewCode(errors.Conflict, "workflow instance already exists").
				WithContext("instance_id", string(instanceID))
		}
		return e.saveInstance(c, st, 0)
	})
}

// StartInstance 启动一个待执行的工作流实例。
func (e *Engine) StartInstance(ctx context.Context, instanceID ID) error {
	return e.StartInstanceWithMutation(ctx, instanceID, nil)
}

// StartInstanceWithMutation 启动实例，并在同一次持久化前执行调用方提供的状态补充逻辑。
func (e *Engine) StartInstanceWithMutation(ctx context.Context, instanceID ID, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadInstanceAndDefinition(ctx, instanceID)
	if err != nil {
		return err
	}
	if st.Status != InstanceStatusPending {
		return errors.NewCode(errors.Conflict, "workflow instance is not pending").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	tctx := &TransitionContext{
		Action:            ActionStart,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            def.StartNodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		st.Status = InstanceStatusRunning
		st.StartedAt = now
		st.UpdatedAt = now
		st.ActiveNodeIDs = []string{def.StartNodeID}
		if st.ActiveNodeStartedAt == nil {
			st.ActiveNodeStartedAt = map[string]time.Time{}
		}
		st.ActiveNodeStartedAt[def.StartNodeID] = now
		st.PendingJoins = []PendingJoin{}
		st.History = append(st.History, HistoryEntry{
			Action: "start",
			NodeID: def.StartNodeID,
			At:     now,
		})
		if err := e.runNodeHooks(c, NodeHookEnter, ActionStart, st, def, def.StartNodeID, "", now); err != nil {
			return err
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
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

	nodeID := st.ActiveNodeIDs[0]
	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		if err := e.advanceNode(c, st, def, nodeID, e.now()); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// AdvanceNode 完成指定活动节点并继续推进工作流。
func (e *Engine) AdvanceNode(ctx context.Context, instanceID ID, nodeID string) error {
	return e.AdvanceNodeWithMutation(ctx, instanceID, nodeID, nil)
}

// AdvanceNodeWithMutation 完成指定活动节点，并在同一次持久化前执行调用方提供的状态补充逻辑。
func (e *Engine) AdvanceNodeWithMutation(ctx context.Context, instanceID ID, nodeID string, mutate StateMutation) error {
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

	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.advanceNode(c, st, def, nodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// AdvanceNodeTo 完成指定活动节点，并只激活给定的单个后继节点。
func (e *Engine) AdvanceNodeTo(ctx context.Context, instanceID ID, nodeID string, nextNodeID string) error {
	return e.AdvanceNodeToWithMutation(ctx, instanceID, nodeID, nextNodeID, nil)
}

// AdvanceNodeToWithMutation 完成指定活动节点，显式选择后继，并在同一次持久化前执行状态补充逻辑。
func (e *Engine) AdvanceNodeToWithMutation(ctx context.Context, instanceID ID, nodeID string, nextNodeID string, mutate StateMutation) error {
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

	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		TargetNodeID:      nextNodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.advanceNodeTo(c, st, def, nodeID, &nextNodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// RejectNode 将指定活动节点驳回到定义允许的目标节点。
func (e *Engine) RejectNode(ctx context.Context, instanceID ID, nodeID string, targetNodeID string) error {
	return e.RejectNodeWithMutation(ctx, instanceID, nodeID, targetNodeID, nil)
}

// RejectNodeWithMutation 将指定活动节点驳回，并在同一次持久化前执行状态补充逻辑。
func (e *Engine) RejectNodeWithMutation(ctx context.Context, instanceID ID, nodeID string, targetNodeID string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}
	if strings.TrimSpace(targetNodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow reject target node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	tctx := &TransitionContext{
		Action:            ActionReject,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		TargetNodeID:      targetNodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.rejectNode(c, st, def, nodeID, targetNodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
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

// validateDefinitionConditions 在保存定义时静态校验全部出边条件表达式。
// 仅当注入的求值器实现了 IConditionValidator 时生效。
func (e *Engine) validateDefinitionConditions(def *Definition) error {
	validator, ok := e.evaluator.(IConditionValidator)
	if !ok || def == nil {
		return nil
	}
	for _, node := range def.Nodes {
		for _, edge := range node.Edges {
			if edge.Condition == "" {
				continue
			}
			if err := validator.ValidateCondition(edge.Condition); err != nil {
				return errors.Wrap(err, errors.InvalidInput, "workflow edge condition is invalid").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("next_node_id", edge.Target)
			}
		}
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

// lockDefinition 获取 definition 级互斥锁，并在释放后清理无人使用的锁对象。
func (e *Engine) lockDefinition(definitionID string) func() {
	return lockKeyed(&e.definitionMu, &e.definitionLocks, definitionID)
}

// lockInstance 获取实例级互斥锁，并在释放后清理无人使用的锁对象。
func (e *Engine) lockInstance(instanceID ID) func() {
	return lockKeyed(&e.instanceMu, &e.instanceLocks, instanceID)
}

func lockKeyed[K comparable](mapMu *sync.Mutex, locks *map[K]*keyedLock[K], key K) func() {
	mapMu.Lock()
	if *locks == nil {
		*locks = map[K]*keyedLock[K]{}
	}
	lock := (*locks)[key]
	if lock == nil {
		lock = &keyedLock[K]{}
		(*locks)[key] = lock
	}
	lock.refs++
	mapMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()

		mapMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(*locks, key)
		}
		mapMu.Unlock()
	}
}

// now 返回当前时钟时间；未显式注入时钟时使用真实时钟。
func (e *Engine) now() time.Time {
	e.clkMu.RLock()
	clk := e.clk
	e.clkMu.RUnlock()
	if clk == nil {
		return clock.NewRealClock().Now()
	}
	return clk.Now()
}

// loadDefinition 读取并重新校验流程定义，防止存储中存在非法图结构。
func (e *Engine) loadDefinition(ctx context.Context, definitionID string, version uint32) (*Definition, error) {
	if strings.TrimSpace(definitionID) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "workflow definition id is empty")
	}

	def, err := e.store.GetDefinition(ctx, definitionID, version)
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

// loadInstanceAndDefinition 读取实例及其绑定的特定版本定义。
func (e *Engine) loadInstanceAndDefinition(ctx context.Context, instanceID ID) (*State, *Definition, error) {
	st, err := e.loadInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(instanceID))
	}

	def, err := e.loadDefinition(ctx, st.DefinitionID, st.DefinitionVersion)
	if err != nil {
		return nil, nil, err
	}
	if def == nil {
		return nil, nil, errors.NewCode(errors.NotFound, "workflow definition not found").
			WithContext("definition_id", st.DefinitionID).
			WithContext("definition_version", st.DefinitionVersion).
			WithContext("instance_id", string(instanceID))
	}
	return st, def, nil
}

// loadRunningInstance 读取运行中实例及其定义，供推进类操作复用。
func (e *Engine) loadRunningInstance(ctx context.Context, instanceID ID) (*State, *Definition, error) {
	st, def, err := e.loadInstanceAndDefinition(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if st.Status != InstanceStatusRunning {
		return nil, nil, errors.NewCode(errors.Conflict, "workflow instance is not running").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}
	return st, def, nil
}
