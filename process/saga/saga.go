// Package saga 提供 Saga 模式实现，用于管理分布式长时事务。
//
// Saga 模式将长时事务拆分为多个本地事务，每个本地事务都有对应的补偿事务。
// 如果某个步骤失败，系统会执行补偿事务回滚之前的操作。
//
// 设计原则：
//   - 通过显式 ICommandExecutor 执行步骤命令。
//   - 复用 EventBus 发布事件。
//   - 最小化接口设计。
//   - 灵活的步骤定义。
package saga

import (
	"context"

	"gochen/messaging/command"
)

// ISaga 定义一个可交给编排器执行的 Saga。
type ISaga interface {
	// ID 返回 Saga 唯一标识
	ID() string

	// Steps 返回 Saga 步骤列表
	Steps() []*SagaStep

	// OnComplete Saga 成功完成时的回调（可选）
	OnComplete(ctx context.Context) error

	// OnFailed Saga 失败时的回调（可选）
	OnFailed(ctx context.Context, err error) error
}

// SagaStep 描述 Saga 中的一个前进步骤及其可选补偿逻辑。
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

// CommandFunc 根据当前上下文构造要执行的命令。
type CommandFunc func(ctx context.Context) (*command.Command, error)

// StepCallback 定义步骤成功或失败时触发的回调。
type StepCallback func(ctx context.Context, stepName string, err error) error

// BaseSaga 为可选回调提供默认空实现。
type BaseSaga struct{}

// OnComplete 是 Saga 成功完成后的默认空回调。
func (b *BaseSaga) OnComplete(ctx context.Context) error {
	return nil
}

// OnFailed 是 Saga 失败后的默认空回调。
func (b *BaseSaga) OnFailed(ctx context.Context, err error) error {
	return nil
}

// NewSagaStep 创建一个带正向命令生成函数的步骤。
func NewSagaStep(name string, commandFunc CommandFunc) *SagaStep {
	return &SagaStep{
		Name:    name,
		Command: commandFunc,
	}
}

// WithCompensation 为步骤补充补偿命令生成函数。
func (s *SagaStep) WithCompensation(compensationFunc CommandFunc) *SagaStep {
	s.Compensation = compensationFunc
	return s
}

// WithOnSuccess 为步骤补充成功回调。
func (s *SagaStep) WithOnSuccess(callback StepCallback) *SagaStep {
	s.OnSuccess = callback
	return s
}

// WithOnFailure 为步骤补充失败回调。
func (s *SagaStep) WithOnFailure(callback StepCallback) *SagaStep {
	s.OnFailure = callback
	return s
}

// HasCompensation 判断当前步骤是否定义了补偿逻辑。
func (s *SagaStep) HasCompensation() bool {
	return s.Compensation != nil
}
