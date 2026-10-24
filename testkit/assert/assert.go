// Package assert 提供基于 Go 标准库实现的纯净测试断言函数。
//
// 零外部依赖，兼容常用断言语法。
package assert

import (
	"errors"
	"fmt"
)

// TestingT 是 testing.T 或 testing.B 的最小接口抽象。
type TestingT interface {
	Errorf(format string, args ...any)
	FailNow()
	Helper()
}

// AnError 是用于测试的通用错误占位变量。
var AnError = errors.New("assert.AnError general error for testing")

func formatMsg(msgAndArgs ...any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}
	if msg, ok := msgAndArgs[0].(string); ok {
		if len(msgAndArgs) == 1 {
			return ": " + msg
		}
		return ": " + fmt.Sprintf(msg, msgAndArgs[1:]...)
	}
	return ": " + fmt.Sprint(msgAndArgs...)
}
