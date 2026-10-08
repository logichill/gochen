package workflow

import (
	"context"

	"gochen/errors"
)

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
