package errors

import (
	"context"
	"fmt"
	"runtime"
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

// WrapDbError 包装数据库错误
// 自动处理常见数据库错误类型
func WrapDbError(ctx context.Context, err error, operation string) error {
	if err == nil {
		return nil
	}

	// 检查是否是NotFound错误
	if IsNotFound(err) {
		return WrapError(err, ErrCodeNotFound, operation)
	}

	// 其他数据库错误：仅做错误包装，不在此处记录日志
	return WrapError(err, ErrCodeDatabase,
		fmt.Sprintf("database operation failed: %s", operation),
	)
}

// New 创建新错误（带调用位置）
func New(code ErrorCode, msg string) error {
	_, file, line, _ := runtime.Caller(1)
	enhancedMsg := fmt.Sprintf("%s (location: %s:%d)", msg, file, line)
	return NewError(code, enhancedMsg)
}
