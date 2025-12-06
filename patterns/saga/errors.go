package saga

import "fmt"

// ErrorCode Saga 错误码
type ErrorCode string

// 预定义错误码常量（不可变）
const (
	ErrCodeSagaNotFound           ErrorCode = "SAGA_NOT_FOUND"
	ErrCodeSagaInvalidState       ErrorCode = "SAGA_INVALID_STATE"
	ErrCodeSagaStepFailed         ErrorCode = "SAGA_STEP_FAILED"
	ErrCodeSagaCompensationFailed ErrorCode = "SAGA_COMPENSATION_FAILED"
	ErrCodeSagaAlreadyCompleted   ErrorCode = "SAGA_ALREADY_COMPLETED"
	ErrCodeSagaAlreadyFailed      ErrorCode = "SAGA_ALREADY_FAILED"
	ErrCodeSagaNoSteps            ErrorCode = "SAGA_NO_STEPS"
	ErrCodeSagaInvalidStep        ErrorCode = "SAGA_INVALID_STEP"
	ErrCodeSagaStoreFailed        ErrorCode = "SAGA_STORE_FAILED"
)

// SagaError Saga 错误
type SagaError struct {
	Code     ErrorCode
	Message  string
	SagaID   string
	StepName string
	Cause    error
}

func (e *SagaError) Error() string {
	var base string
	if e.SagaID != "" && e.StepName != "" {
		base = fmt.Sprintf("%s: %s (saga=%s, step=%s)", e.Code, e.Message, e.SagaID, e.StepName)
	} else if e.SagaID != "" {
		base = fmt.Sprintf("%s: %s (saga=%s)", e.Code, e.Message, e.SagaID)
	} else {
		base = fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", base, e.Cause)
	}
	return base
}

func (e *SagaError) Unwrap() error { return e.Cause }

// Is 实现 errors.Is 接口，基于错误码匹配
func (e *SagaError) Is(target error) bool {
	t, ok := target.(*SagaError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// 哨兵错误（仅用于 errors.Is 比较，不应直接返回）
var (
	errSagaNotFound           = &SagaError{Code: ErrCodeSagaNotFound}
	errSagaInvalidState       = &SagaError{Code: ErrCodeSagaInvalidState}
	errSagaStepFailed         = &SagaError{Code: ErrCodeSagaStepFailed}
	errSagaCompensationFailed = &SagaError{Code: ErrCodeSagaCompensationFailed}
	errSagaAlreadyCompleted   = &SagaError{Code: ErrCodeSagaAlreadyCompleted}
	errSagaAlreadyFailed      = &SagaError{Code: ErrCodeSagaAlreadyFailed}
	errSagaNoSteps            = &SagaError{Code: ErrCodeSagaNoSteps}
	errSagaInvalidStep        = &SagaError{Code: ErrCodeSagaInvalidStep}
	errSagaStoreFailed        = &SagaError{Code: ErrCodeSagaStoreFailed}
)

// ========== 哨兵错误访问函数（用于 errors.Is 比较）==========

// ErrSagaNotFound 返回 Saga 未找到错误（用于 errors.Is 比较）
func ErrSagaNotFound() *SagaError { return errSagaNotFound }

// ErrSagaInvalidState 返回 Saga 状态无效错误（用于 errors.Is 比较）
func ErrSagaInvalidState() *SagaError { return errSagaInvalidState }

// ErrSagaStepFailed 返回 Saga 步骤失败错误（用于 errors.Is 比较）
func ErrSagaStepFailed() *SagaError { return errSagaStepFailed }

// ErrSagaCompensationFailed 返回 Saga 补偿失败错误（用于 errors.Is 比较）
func ErrSagaCompensationFailed() *SagaError { return errSagaCompensationFailed }

// ErrSagaAlreadyCompleted 返回 Saga 已完成错误（用于 errors.Is 比较）
func ErrSagaAlreadyCompleted() *SagaError { return errSagaAlreadyCompleted }

// ErrSagaAlreadyFailed 返回 Saga 已失败错误（用于 errors.Is 比较）
func ErrSagaAlreadyFailed() *SagaError { return errSagaAlreadyFailed }

// ErrSagaNoSteps 返回 Saga 无步骤错误（用于 errors.Is 比较）
func ErrSagaNoSteps() *SagaError { return errSagaNoSteps }

// ErrSagaInvalidStep 返回 Saga 步骤无效错误（用于 errors.Is 比较）
func ErrSagaInvalidStep() *SagaError { return errSagaInvalidStep }

// ErrSagaStoreFailed 返回 Saga 存储失败错误（用于 errors.Is 比较）
func ErrSagaStoreFailed() *SagaError { return errSagaStoreFailed }

// ========== 工厂函数（创建带上下文的错误实例）==========

// NewSagaNotFoundError 创建 Saga 未找到错误
func NewSagaNotFoundError(sagaID string) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaNotFound,
		Message: "saga not found",
		SagaID:  sagaID,
	}
}

// NewSagaInvalidStateError 创建 Saga 状态无效错误
func NewSagaInvalidStateError(sagaID string, currentState, expectedState string) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaInvalidState,
		Message: fmt.Sprintf("invalid state: current=%s, expected=%s", currentState, expectedState),
		SagaID:  sagaID,
	}
}

// NewSagaStepFailedError 创建 Saga 步骤失败错误
func NewSagaStepFailedError(sagaID, stepName string, cause error) *SagaError {
	return &SagaError{
		Code:     ErrCodeSagaStepFailed,
		Message:  "step execution failed",
		SagaID:   sagaID,
		StepName: stepName,
		Cause:    cause,
	}
}

// NewSagaCompensationFailedError 创建 Saga 补偿失败错误
func NewSagaCompensationFailedError(sagaID, stepName string, cause error) *SagaError {
	return &SagaError{
		Code:     ErrCodeSagaCompensationFailed,
		Message:  "compensation failed",
		SagaID:   sagaID,
		StepName: stepName,
		Cause:    cause,
	}
}

// NewSagaAlreadyCompletedError 创建 Saga 已完成错误
func NewSagaAlreadyCompletedError(sagaID string) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaAlreadyCompleted,
		Message: "saga already completed",
		SagaID:  sagaID,
	}
}

// NewSagaAlreadyFailedError 创建 Saga 已失败错误
func NewSagaAlreadyFailedError(sagaID string) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaAlreadyFailed,
		Message: "saga already failed",
		SagaID:  sagaID,
	}
}

// NewSagaNoStepsError 创建 Saga 无步骤错误
func NewSagaNoStepsError(sagaID string) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaNoSteps,
		Message: "saga has no steps",
		SagaID:  sagaID,
	}
}

// NewSagaInvalidStepError 创建 Saga 步骤无效错误
func NewSagaInvalidStepError(sagaID, stepName, reason string) *SagaError {
	return &SagaError{
		Code:     ErrCodeSagaInvalidStep,
		Message:  reason,
		SagaID:   sagaID,
		StepName: stepName,
	}
}

// NewSagaStoreFailedError 创建 Saga 存储失败错误
func NewSagaStoreFailedError(sagaID string, cause error) *SagaError {
	return &SagaError{
		Code:    ErrCodeSagaStoreFailed,
		Message: "saga store operation failed",
		SagaID:  sagaID,
		Cause:   cause,
	}
}
