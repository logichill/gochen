// Package saga 提供 Saga 模式实现，用于管理分布式长时事务
//
// Saga 模式将长时事务拆分为多个本地事务，每个本地事务都有对应的补偿事务。
// 如果某个步骤失败，系统会执行补偿事务回滚之前的操作。
//
// 设计原则：
//   - 复用 CommandBus（Phase 1）执行命令
//   - 复用 EventBus 发布事件
//   - 最小化接口设计
//   - 灵活的步骤定义
package saga

import (
	"context"

	"gochen/messaging/command"
)

// ISaga Saga 接口
//
// 定义 Saga 的基本行为。用户实现此接口来定义自己的 Saga。
//
// 示例：
//
//	type OrderSaga struct {
//	    orderID string
//	}
//
//	func (s *OrderSaga) GetID() string {
//	    return s.orderID
//	}
//
//	func (s *OrderSaga) GetSteps() []*SagaStep {
//	    return []*SagaStep{
//	        { Name: "CreateOrder", Command: ... },
//	        { Name: "ReserveInventory", Command: ... },
//	    }
//	}
type ISaga interface {
	// GetID 返回 Saga 唯一标识
	GetID() string

	// GetSteps 返回 Saga 步骤列表
	GetSteps() []*SagaStep

	// OnComplete Saga 成功完成时的回调（可选）
	OnComplete(ctx context.Context) error

	// OnFailed Saga 失败时的回调（可选）
	OnFailed(ctx context.Context, err error) error
}

// SagaStep Saga 步骤
//
// 每个步骤包含一个正向命令和一个可选的补偿命令。
//
// 特性：
//   - 使用函数而非接口（灵活）
//   - 补偿命令是可选的
//   - 支持成功/失败回调
//   - Name 在同一个 Saga 内应保持唯一，用于标识步骤与记录执行状态
type SagaStep struct {
	// Name 步骤名称（唯一标识）
	Name string

	// Command 正向命令生成函数
	//
	// 返回要执行的命令。如果返回 error，步骤将失败。
	Command CommandFunc

	// Compensation 补偿命令生成函数（可选）
	//
	// 当后续步骤失败时，会执行此补偿命令来回滚操作。
	// 如果为 nil，表示该步骤不需要补偿。
	Compensation CommandFunc

	// OnSuccess 步骤成功时的回调（可选）
	//
	// 可用于记录日志、更新状态等。
	OnSuccess StepCallback

	// OnFailure 步骤失败时的回调（可选）
	//
	// 可用于记录错误、发送告警等。
	OnFailure StepCallback
}

// CommandFunc 命令生成函数
//
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - *command.Command: 要执行的命令（复用 Phase 1）
//   - error: 生成失败错误
type CommandFunc func(ctx context.Context) (*command.Command, error)

// StepCallback 步骤回调函数
//
// 参数：
//   - ctx: 上下文
//   - stepName: 步骤名称
//   - err: 错误（失败时）
//
// 返回：
//   - error: 回调错误
type StepCallback func(ctx context.Context, stepName string, err error) error

// BaseSaga 基础 Saga 实现
//
// 提供默认的回调实现，用户可以嵌入此结构体以减少代码。
//
// 示例：
//
//	type MySaga struct {
//	    saga.BaseSaga
//	    id string
//	}
//
//	func (s *MySaga) GetID() string {
//	    return s.id
//	}
type BaseSaga struct{}

// OnComplete 默认完成回调（无操作）
func (b *BaseSaga) OnComplete(ctx context.Context) error {
	return nil
}

// OnFailed 默认失败回调（无操作）
func (b *BaseSaga) OnFailed(ctx context.Context, err error) error {
	return nil
}

// NewSagaStep 创建 Saga 步骤
//
// 参数：
//   - name: 步骤名称
//   - commandFunc: 命令生成函数
//
// 返回：
//   - *SagaStep: 步骤实例
func NewSagaStep(name string, commandFunc CommandFunc) *SagaStep {
	return &SagaStep{
		Name:    name,
		Command: commandFunc,
	}
}

// WithCompensation 添加补偿命令
//
// 参数：
//   - compensationFunc: 补偿命令生成函数
//
// 返回：
//   - *SagaStep: 步骤实例（支持链式调用）
func (s *SagaStep) WithCompensation(compensationFunc CommandFunc) *SagaStep {
	s.Compensation = compensationFunc
	return s
}

// WithOnSuccess 添加成功回调
//
// 参数：
//   - callback: 成功回调函数
//
// 返回：
//   - *SagaStep: 步骤实例（支持链式调用）
func (s *SagaStep) WithOnSuccess(callback StepCallback) *SagaStep {
	s.OnSuccess = callback
	return s
}

// WithOnFailure 添加失败回调
//
// 参数：
//   - callback: 失败回调函数
//
// 返回：
//   - *SagaStep: 步骤实例（支持链式调用）
func (s *SagaStep) WithOnFailure(callback StepCallback) *SagaStep {
	s.OnFailure = callback
	return s
}

// HasCompensation 检查是否有补偿命令
//
// 返回：
//   - bool: 是否有补偿
func (s *SagaStep) HasCompensation() bool {
	return s.Compensation != nil
}
