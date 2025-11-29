package saga

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging/command"
)

func sagaLogger() logging.Logger {
	return logging.GetLogger().WithFields(
		logging.String("component", "saga.orchestrator"),
	)
}

// SagaOrchestrator Saga 编排器
//
// 负责执行 Saga 的所有步骤，包括正向执行和补偿。
//
// 特性：
//   - 复用 CommandBus（Phase 1）执行命令
//   - 复用 EventBus 发布事件
//   - 自动补偿机制
//   - 状态持久化
//   - 支持恢复
//
// 注意：
//   - Saga 依赖 CommandBus.Dispatch 的返回值来判断步骤是否成功；要获得可靠的错误语义，CommandBus 应该基于同步 Transport
//     （例如 messaging/transport/sync），否则在异步 Transport 下，Dispatch 的 error 仅能反映“消息是否进入传输层”，而不能保证 handler 的业务错误被感知。
type SagaOrchestrator struct {
	commandBus *command.CommandBus
	eventBus   bus.IEventBus
	stateStore ISagaStateStore
}

// NewSagaOrchestrator 创建 Saga 编排器
//
// 参数：
//   - commandBus: 命令总线（复用 Phase 1）
//   - eventBus: 事件总线
//   - stateStore: 状态存储（可选，nil 表示不持久化）
//
// 返回：
//   - *SagaOrchestrator: 编排器实例
func NewSagaOrchestrator(
	commandBus *command.CommandBus,
	eventBus bus.IEventBus,
	stateStore ISagaStateStore,
) *SagaOrchestrator {
	return &SagaOrchestrator{
		commandBus: commandBus,
		eventBus:   eventBus,
		stateStore: stateStore,
	}
}

// Execute 执行 Saga
//
// 从第一个步骤开始顺序执行所有步骤。
// 如果某个步骤失败，会自动执行补偿。
//
// 参数：
//   - ctx: 上下文
//   - saga: Saga 实例
//
// 返回：
//   - error: 执行失败错误
func (o *SagaOrchestrator) Execute(ctx context.Context, saga ISaga) error {
	sagaID := saga.GetID()
	steps := saga.GetSteps()

	if len(steps) == 0 {
		return ErrSagaNoSteps
	}

	sagaLogger().Info(ctx, "开始执行 Saga",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	// 创建初始状态
	state := NewSagaState(sagaID, fmt.Sprintf("%T", saga))
	state.Status = SagaStatusRunning

	// 保存初始状态
	if o.stateStore != nil {
		if err := o.stateStore.Save(ctx, state); err != nil {
			sagaLogger().Warn(ctx, "保存 Saga 状态失败", logging.Error(err))
			// 继续执行
		}
	}

	// 发布 Saga 开始事件
	o.publishEvent(ctx, EventSagaStarted, sagaID, nil)

	// 执行步骤
	for i, step := range steps {
		sagaLogger().Info(ctx, "执行 Saga 步骤",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step_name", step.Name))

		// 执行步骤
		if err := o.executeStep(ctx, step, state); err != nil {
			sagaLogger().Error(ctx, "Saga 步骤失败", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step_name", step.Name))

			// 标记失败
			state.MarkStepFailed(step.Name, err)
			if o.stateStore != nil {
				o.stateStore.Update(ctx, state)
			}

			// 发布步骤失败事件
			o.publishEvent(ctx, EventSagaStepFailed, sagaID, map[string]any{
				"step":  step.Name,
				"error": err.Error(),
			})

			// 执行补偿
			if compErr := o.compensate(ctx, saga, state, i); compErr != nil {
				sagaLogger().Error(ctx, "Saga 补偿失败", logging.Error(compErr),
					logging.String("saga_id", sagaID))

				// 调用失败回调
				saga.OnFailed(ctx, fmt.Errorf("step failed and compensation failed: %w", errors.Join(err, compErr)))

				// 发布 Saga 失败事件
				o.publishEvent(ctx, EventSagaFailed, sagaID, map[string]any{
					"error":              err.Error(),
					"compensation_error": compErr.Error(),
				})

				return errors.Join(ErrSagaStepFailed, err, compErr)
			}

			// 调用失败回调
			saga.OnFailed(ctx, err)

			// 发布 Saga 补偿完成事件
			o.publishEvent(ctx, EventSagaCompensationCompleted, sagaID, map[string]any{
				"error": err.Error(),
			})

			return errors.Join(ErrSagaStepFailed, err)
		}

		// 标记步骤完成
		state.MarkStepCompleted(step.Name)
		if o.stateStore != nil {
			o.stateStore.Update(ctx, state)
		}

		// 发布步骤完成事件
		o.publishEvent(ctx, EventSagaStepCompleted, sagaID, map[string]any{
			"step": step.Name,
		})
	}

	// 所有步骤完成
	state.MarkCompleted()
	if o.stateStore != nil {
		o.stateStore.Update(ctx, state)
	}

	// 调用完成回调
	if err := saga.OnComplete(ctx); err != nil {
		sagaLogger().Warn(ctx, "Saga 完成回调失败", logging.Error(err),
			logging.String("saga_id", sagaID))
		// 不影响 Saga 成功状态
	}

	// 发布 Saga 完成事件
	o.publishEvent(ctx, EventSagaCompleted, sagaID, nil)

	sagaLogger().Info(ctx, "Saga 执行完成",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	return nil
}

// executeStep 执行单个步骤
func (o *SagaOrchestrator) executeStep(ctx context.Context, step *SagaStep, state *SagaState) error {
	// 生成命令
	cmd, err := step.Command(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate command: %w", err)
	}

	if cmd == nil {
		return fmt.Errorf("command is nil")
	}

	// 使用 CommandBus 执行命令（复用 Phase 1）
	if err := o.commandBus.Dispatch(ctx, cmd); err != nil {
		// 调用失败回调
		if step.OnFailure != nil {
			step.OnFailure(ctx, step.Name, err)
		}
		return err
	}

	// 调用成功回调
	if step.OnSuccess != nil {
		if err := step.OnSuccess(ctx, step.Name, nil); err != nil {
			sagaLogger().Warn(ctx, "步骤成功回调失败", logging.Error(err),
				logging.String("step", step.Name))
			// 不影响步骤成功
		}
	}

	return nil
}

// compensate 执行补偿
func (o *SagaOrchestrator) compensate(ctx context.Context, saga ISaga, state *SagaState, failedStepIndex int) error {
	sagaID := saga.GetID()
	steps := saga.GetSteps()

	sagaLogger().Info(ctx, "开始补偿 Saga",
		logging.String("saga_id", sagaID),
		logging.Int("failed_step_index", failedStepIndex))

	// 标记补偿中
	state.MarkCompensating()
	if o.stateStore != nil {
		o.stateStore.Update(ctx, state)
	}

	// 发布补偿开始事件
	o.publishEvent(ctx, EventSagaCompensationStarted, sagaID, nil)

	// 倒序执行补偿（从失败步骤的前一个开始）
	for i := failedStepIndex - 1; i >= 0; i-- {
		step := steps[i]

		// 如果没有补偿命令，跳过
		if !step.HasCompensation() {
			sagaLogger().Info(ctx, "步骤无补偿命令，跳过",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			continue
		}

		sagaLogger().Info(ctx, "执行补偿",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step", step.Name))

		// 生成补偿命令
		compCmd, err := step.Compensation(ctx)
		if err != nil {
			sagaLogger().Error(ctx, "生成补偿命令失败", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return fmt.Errorf("%w: failed to generate compensation command for step %s: %v",
				ErrSagaCompensationFailed, step.Name, err)
		}

		if compCmd == nil {
			sagaLogger().Error(ctx, "补偿命令为 nil",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return fmt.Errorf("%w: compensation command is nil for step %s",
				ErrSagaCompensationFailed, step.Name)
		}

		// 执行补偿命令
		if err := o.commandBus.Dispatch(ctx, compCmd); err != nil {
			sagaLogger().Error(ctx, "执行补偿命令失败", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))

			// 发布补偿失败事件
			o.publishEvent(ctx, EventSagaCompensationStepFailed, sagaID, map[string]any{
				"step":  step.Name,
				"error": err.Error(),
			})

			return fmt.Errorf("%w: failed to execute compensation for step %s: %v",
				ErrSagaCompensationFailed, step.Name, err)
		}

		// 发布补偿步骤完成事件
		o.publishEvent(ctx, EventSagaStepCompleted, sagaID, map[string]any{
			"step": step.Name,
		})
	}

	// 标记补偿完成
	state.MarkCompensated()
	if o.stateStore != nil {
		o.stateStore.Update(ctx, state)
	}

	sagaLogger().Info(ctx, "Saga 补偿完成",
		logging.String("saga_id", sagaID))

	return nil
}

// publishEvent 发布事件
type sagaEventPayload struct {
	SagaID    string         `json:"saga_id"`
	Step      string         `json:"step,omitempty"`
	Status    string         `json:"status,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Extra     map[string]any `json:"extra,omitempty"`
}

func (o *SagaOrchestrator) publishEvent(ctx context.Context, eventType string, sagaID string, data map[string]any) {
	if o.eventBus == nil {
		return
	}

	payload := sagaEventPayload{
		SagaID:    sagaID,
		Status:    eventType,
		Timestamp: time.Now(),
	}
	if data != nil {
		payload.Extra = data
		if step, ok := data["step"].(string); ok {
			payload.Step = step
		}
		if errStr, ok := data["error"].(string); ok {
			payload.Error = errStr
		}
	}

	evt := eventing.NewEvent(0, "Saga", eventType, uint64(payload.Timestamp.UnixNano()), payload)
	meta := evt.GetMetadata()
	meta["saga_id"] = sagaID
	meta["status"] = eventType
	if payload.Step != "" {
		meta["step"] = payload.Step
	}

	if err := o.eventBus.PublishEvent(ctx, evt); err != nil {
		sagaLogger().Warn(ctx, "发布 Saga 事件失败", logging.Error(err),
			logging.String("event_type", eventType),
			logging.String("saga_id", sagaID))
		return
	}

	sagaLogger().Debug(ctx, "发布 Saga 事件",
		logging.String("event_type", eventType),
		logging.String("saga_id", sagaID))
}

// Resume 从状态恢复 Saga 执行
//
// 用于进程重启后继续执行 Saga。
//
// 参数：
//   - ctx: 上下文
//   - saga: Saga 实例
//   - state: 保存的状态
//
// 返回：
//   - error: 恢复失败错误
func (o *SagaOrchestrator) Resume(ctx context.Context, saga ISaga, state *SagaState) error {
	sagaID := saga.GetID()

	// 检查状态
	if state.IsCompleted() {
		return ErrSagaAlreadyCompleted
	}
	if state.IsFailed() || state.IsCompensated() {
		return ErrSagaAlreadyFailed
	}

	sagaLogger().Info(ctx, "恢复 Saga 执行",
		logging.String("saga_id", sagaID),
		logging.Int("current_step", state.CurrentStep))

	steps := saga.GetSteps()

	// 从当前步骤继续执行
	for i := state.CurrentStep; i < len(steps); i++ {
		step := steps[i]

		if err := o.executeStep(ctx, step, state); err != nil {
			state.MarkStepFailed(step.Name, err)
			if o.stateStore != nil {
				o.stateStore.Update(ctx, state)
			}

			// 执行补偿
			if compErr := o.compensate(ctx, saga, state, i); compErr != nil {
				saga.OnFailed(ctx, fmt.Errorf("step failed and compensation failed: %w", errors.Join(err, compErr)))
				return errors.Join(ErrSagaStepFailed, err, compErr)
			}

			saga.OnFailed(ctx, err)
			return errors.Join(ErrSagaStepFailed, err)
		}

		state.MarkStepCompleted(step.Name)
		if o.stateStore != nil {
			o.stateStore.Update(ctx, state)
		}
	}

	// 完成
	state.MarkCompleted()
	if o.stateStore != nil {
		o.stateStore.Update(ctx, state)
	}

	saga.OnComplete(ctx)

	sagaLogger().Info(ctx, "Saga 恢复执行完成",
		logging.String("saga_id", sagaID))

	return nil
}
