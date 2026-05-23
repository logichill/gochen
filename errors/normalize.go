package errors

// IErrorCoder 错误码接口。
//
// 任何实现此接口的错误类型都可以被 Normalize 函数自动转换为对应的 AppError。
// 这个接口用于扩展错误处理机制，支持第三方库错误和自定义业务错误的自动转换。
//
// 使用场景：
//  1. 集成第三方库错误（Redis、gRPC、MQ 客户端等）
//  2. 自定义业务模块错误（保留领域语义，同时支持统一处理）
type IErrorCoder interface {
	// ErrorCode 返回错误对应的通用错误码
	ErrorCode() ErrorCode
}

// Normalize 规范化输入。
func Normalize(err error) error {
	if err == nil {
		return nil
	}
	// 直接装箱的 typed-nil *AppError 应保持 nil 语义，避免被“规范化”为非 nil error。
	if appErr, ok := err.(*AppError); ok && appErr == nil {
		return nil
	}

	// 已经是 AppError（或链路中包含 AppError），视为已规范化
	if _, ok := findAppError(err); ok {
		return err
	}

	// 实现 IErrorCoder 接口的错误（自动转换）
	var coder IErrorCoder
	if As(err, &coder) && coder != nil {
		return Wrap(err, coder.ErrorCode(), err.Error())
	}

	if Is(err, ErrUnsupported) {
		return Wrap(err, Unsupported, err.Error())
	}

	// 未识别的错误保持原样
	return err
}
