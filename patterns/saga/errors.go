package saga

import "errors"

// Saga 相关错误
var (
	// ErrSagaNotFound Saga 不存在
	ErrSagaNotFound = errors.New("saga not found")

	// ErrSagaInvalidState Saga 状态无效
	ErrSagaInvalidState = errors.New("saga invalid state")

	// ErrSagaStepFailed Saga 步骤失败
	ErrSagaStepFailed = errors.New("saga step failed")

	// ErrSagaCompensationFailed Saga 补偿失败
	ErrSagaCompensationFailed = errors.New("saga compensation failed")

	// ErrSagaAlreadyCompleted Saga 已完成
	ErrSagaAlreadyCompleted = errors.New("saga already completed")

	// ErrSagaAlreadyFailed Saga 已失败
	ErrSagaAlreadyFailed = errors.New("saga already failed")

	// ErrSagaNoSteps Saga 没有步骤
	ErrSagaNoSteps = errors.New("saga has no steps")

	// ErrSagaInvalidStep Saga 步骤无效
	ErrSagaInvalidStep = errors.New("saga invalid step")

	// ErrSagaStoreFailed Saga 存储失败
	ErrSagaStoreFailed = errors.New("saga store failed")
)
