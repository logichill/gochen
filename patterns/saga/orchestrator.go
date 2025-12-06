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
//     （例如 messaging/transport/sync），否则在异步 Transport 下，Dispatch 的 error 仅能反映"消息是否进入传输层"，而不能保证 handler 的业务错误被感知。
//
// # 并发模型与线程安全
//
// ⚠️ 同一个 Saga 实例在并发场景下不是 goroutine 安全的：
//   - 对同一 sagaID 并发调用 Execute() 会导致状态竞争；
//   - Orchestrator 内部没有加锁，默认假设“同一 Saga 实例在单线程中顺序执行”。
//
// 安全的使用方式：
//   - ✅ 不同 sagaID 的 Saga 可以并发执行；
//   - ✅ 同一 sagaID 的 Execute() 调用必须串行；
//   - ❌ 不要对同一 sagaID 并发调用 Execute()。
//
// 在生产环境中，如果需要同一 Saga ID 在多实例或多线程下并发调度：
//   - 建议在 Orchestrator 外部实现分布式锁（例如 Redis 锁、数据库 advisory lock 等）；
//   - 或者通过工作队列将同一 sagaID 的请求串行化。
//
// # 可选增强：分布式锁提供者接口
//
// 如果希望在框架层内置锁能力，可以考虑引入如下接口：
//
//	type ILockProvider interface {
//	    // Acquire 尝试为给定 sagaID 获取锁，返回释放函数与错误信息
//	    Acquire(ctx context.Context, sagaID string) (release func(), err error)
//	}
//
// 可能的实现方式：
//   - 基于 Redis 的 SET NX + 过期时间；
//   - 基于数据库的 advisory lock 或 SELECT FOR UPDATE；
//   - 基于 Etcd/Consul 的原生锁原语。
//
// 集成点：在 Execute() 入口处先通过 ILockProvider 获取锁，执行完成后释放。
type SagaOrchestrator struct {
	commandBus *command.CommandBus
	eventBus   bus.IEventBus
	stateStore ISagaStateStore
	logger     logging.ILogger
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
	o := &SagaOrchestrator{
		commandBus: commandBus,
		eventBus:   eventBus,
		stateStore: stateStore,
	}
	o.logger = logging.ComponentLogger("saga.orchestrator")
	return o
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

	o.logger.Info(ctx, "starting saga execution",
		logging.String("saga_id", sagaID),
		logging.Int("steps", len(steps)))

	// 创建初始状态
	state := NewSagaState(sagaID, fmt.Sprintf("%T", saga))
	state.Status = SagaStatusRunning

	// Save initial state
	if o.stateStore != nil {
		if err := o.stateStore.Save(ctx, state); err != nil {
			// State persistence failure is a serious issue that may prevent saga recovery
			o.logger.Error(ctx, "failed to save saga state", logging.Error(err))
			// continue execution - saga will still proceed but won't be recoverable on crash
		}
	}

	// 发布 Saga 开始事件
	o.publishEvent(ctx, EventSagaStarted, sagaID, nil)

	// 执行步骤
	for i, step := range steps {
		o.logger.Info(ctx, "executing saga step",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step_name", step.Name))

		// 执行步骤
		if err := o.executeStep(ctx, step, state); err != nil {
			o.logger.Error(ctx, "saga step failed", logging.Error(err),
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
				o.logger.Error(ctx, "saga compensation failed", logging.Error(compErr),
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
		o.logger.Warn(ctx, "saga completion callback failed", logging.Error(err),
			logging.String("saga_id", sagaID))
		// don't affect saga success status
	}

	// 发布 Saga 完成事件
	o.publishEvent(ctx, EventSagaCompleted, sagaID, nil)

	o.logger.Info(ctx, "saga execution completed",
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
			o.logger.Warn(ctx, "step success callback failed", logging.Error(err),
				logging.String("step", step.Name))
			// don't affect step success
		}
	}

	return nil
}

// compensate 执行补偿
func (o *SagaOrchestrator) compensate(ctx context.Context, saga ISaga, state *SagaState, failedStepIndex int) error {
	sagaID := saga.GetID()
	steps := saga.GetSteps()

	o.logger.Info(ctx, "starting saga compensation",
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
			o.logger.Info(ctx, "step has no compensation, skipping",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			continue
		}

		o.logger.Info(ctx, "executing compensation",
			logging.String("saga_id", sagaID),
			logging.Int("step_index", i),
			logging.String("step", step.Name))

		// 生成补偿命令
		compCmd, err := step.Compensation(ctx)
		if err != nil {
			o.logger.Error(ctx, "failed to generate compensation command", logging.Error(err),
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return fmt.Errorf("%w: failed to generate compensation command for step %s: %v",
				ErrSagaCompensationFailed, step.Name, err)
		}

		if compCmd == nil {
			o.logger.Error(ctx, "compensation command is nil",
				logging.String("saga_id", sagaID),
				logging.String("step", step.Name))
			return fmt.Errorf("%w: compensation command is nil for step %s",
				ErrSagaCompensationFailed, step.Name)
		}

		// 执行补偿命令
		if err := o.commandBus.Dispatch(ctx, compCmd); err != nil {
			o.logger.Error(ctx, "failed to execute compensation command", logging.Error(err),
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

	o.logger.Info(ctx, "saga compensation completed",
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
		o.logger.Warn(ctx, "failed to publish saga event", logging.Error(err),
			logging.String("event_type", eventType),
			logging.String("saga_id", sagaID))
		return
	}

	o.logger.Debug(ctx, "saga event published",
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

	o.logger.Info(ctx, "resuming saga execution",
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

	o.logger.Info(ctx, "saga resume execution completed",
		logging.String("saga_id", sagaID))

	return nil
}
