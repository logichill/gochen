package errors

import (
	"context"
	"fmt"
	"runtime"

	"gochen/logging"
)

// Wrap 包装错误，添加错误码和上下文信息
// 建议：在Service/Handler层边界使用，添加业务上下文
func Wrap(_ context.Context, err error, code ErrorCode, msg string) error {
	if err == nil {
		return nil
	}

	// 仅包装错误，不做隐式日志记录
	return WrapError(err, code, msg)
}

// WrapWithLog 包装错误并记录警告日志
// 建议：用于需要立即记录的错误场景
func WrapWithLog(ctx context.Context, err error, code ErrorCode, msg string, fields ...logging.Field) error {
	if err == nil {
		return nil
	}

	// 获取调用位置
	_, file, line, _ := runtime.Caller(1)

	// 创建增强错误
	wrapped := WrapError(err, code, msg)

	// 记录警告日志
	allFields := append([]logging.Field{
		logging.Error(err),
		logging.String("error_code", string(code)),
		logging.String("location", fmt.Sprintf("%s:%d", file, line)),
	}, fields...)

	logging.GetLogger().Warn(ctx, msg, allFields...)

	return wrapped
}

// WrapDatabaseError 包装数据库错误
// 自动处理常见数据库错误类型
func WrapDatabaseError(ctx context.Context, err error, operation string) error {
	if err == nil {
		return nil
	}

	// 检查是否是NotFound错误
	if IsNotFound(err) {
		return WrapError(err, ErrCodeNotFound, operation)
	}

	// 其他数据库错误
	return WrapWithLog(ctx, err, ErrCodeDatabase,
		fmt.Sprintf("database operation failed: %s", operation),
		logging.String("operation", operation),
	)
}

// New 创建新错误（带调用位置）
func New(code ErrorCode, msg string) error {
	_, file, line, _ := runtime.Caller(1)
	enhancedMsg := fmt.Sprintf("%s (location: %s:%d)", msg, file, line)
	return NewError(code, enhancedMsg)
}

// NewValidationError 创建新的验证错误
func NewValidationError(msg string) error {
	return New(ErrCodeValidation, msg)
}
