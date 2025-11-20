package middleware

import (
	"context"
	"fmt"

	"gochen/messaging"
	"gochen/messaging/command"
)

// IValidator 验证器接口（接口隔离）
//
// 定义最小化的验证接口，第三方可以实现自己的验证器
type IValidator interface {
	// Struct 验证结构体
	Struct(s any) error
}

// ValidationMiddleware 命令验证中间件
//
// 在命令执行前验证 Payload 的有效性。
// 只对命令类型的消息执行验证，其他消息类型直接透传。
//
// 特性：
//   - 接受 IValidator 接口（可复用 validation.IValidator 实现）
//   - 实现 messaging.IMiddleware 接口
//   - 自动识别命令消息
//   - 验证失败返回清晰的错误
type ValidationMiddleware struct {
	validator IValidator
}

// NewValidationMiddleware 创建验证中间件
//
// 参数：
//   - validator: 验证器实例（实现 IValidator 接口）
//
// 返回：
//   - *ValidationMiddleware: 中间件实例
func NewValidationMiddleware(validator IValidator) *ValidationMiddleware {
	return &ValidationMiddleware{
		validator: validator,
	}
}

// Handle 实现 messaging.IMiddleware 接口
//
// 执行流程：
//  1. 检查消息类型，只处理命令
//  2. 提取命令的 Payload
//  3. 使用 validator 验证 Payload
//  4. 验证通过则调用 next 继续执行链
//  5. 验证失败则返回错误，中断执行链
func (m *ValidationMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 只处理命令消息
	if message.GetType() != messaging.MessageTypeCommand {
		return next(ctx, message)
	}

	// 尝试类型断言为 Command
	cmd, ok := message.(*command.Command)
	if !ok {
		// 不是 Command 类型，直接透传
		return next(ctx, message)
	}

	// 获取 Payload
	payload := cmd.GetPayload()
	if payload == nil {
		// Payload 为空，跳过验证
		return next(ctx, message)
	}

	// 使用 validator 验证 Payload
	if err := m.validator.Struct(payload); err != nil {
		return fmt.Errorf("command validation failed for %s: %w", cmd.GetCommandType(), err)
	}

	// 验证通过，继续执行
	return next(ctx, message)
}

// Name 实现 messaging.IMiddleware 接口
func (m *ValidationMiddleware) Name() string {
	return "CommandValidation"
}
