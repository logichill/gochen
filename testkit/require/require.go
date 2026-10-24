// Package require 提供基于 assert 实现并在失败时立即终止测试的断言包。
//
// 零外部依赖，等同于 testing.T.Fatalf / FailNow 行为。
package require

import (
	"gochen/testkit/assert"
)

// TestingT 抽象 testing.T 或 testing.B。
type TestingT = assert.TestingT

// AnError 通用测试错误占位。
var AnError = assert.AnError
