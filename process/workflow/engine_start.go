package workflow

import (
	"context"
	"strings"

	"time"

	"gochen/errors"
)

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

func initialInstanceData(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return cloneWorkflowMap(value)
}
