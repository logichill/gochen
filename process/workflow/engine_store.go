package workflow

import (
	"context"
	"strings"

	"gochen/errors"
)

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
