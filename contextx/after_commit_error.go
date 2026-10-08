package contextx

import (
	"gochen/errors"
)

// AfterCommitError 表示主操作已经成功，后续回调、发布、快照或结果记录失败。
// 调用方不得据此重试整个主操作；应重试失败的后续阶段或核对已完成的结果。
type AfterCommitError struct {
	cause error
}

// Error 实现 error 接口。
func (e *AfterCommitError) Error() string {
	if e == nil || e.cause == nil {
		return "after-commit step failed"
	}
	return "after-commit step failed: " + e.cause.Error()
}

func (e *AfterCommitError) Unwrap() error { return e.cause }

// WrapAfterCommitError 把提交后回调错误标记为“提交已成功”。
func WrapAfterCommitError(err error) error {
	if err == nil || IsAfterCommitError(err) {
		return err
	}
	return &AfterCommitError{cause: err}
}

// IsAfterCommitError 判断 err 是否表示“主操作成功但后续步骤失败”。
func IsAfterCommitError(err error) bool {
	var target *AfterCommitError
	return errors.As(err, &target)
}
