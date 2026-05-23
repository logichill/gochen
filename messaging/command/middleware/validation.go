package middleware

import (
	"context"

	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
	"gochen/validate"
)

// ValidationMiddleware 命令验证中间件。
//
// 在命令执行前验证 Payload 的有效性。
// 只对命令类型的消息执行验证，其他消息类型直接透传。
//
// 特性：
//   - 接受 validate.IValidator 接口（可复用 API/app 层注入的 validator 实现）
//   - 实现 messaging.IMiddleware 接口。
//   - 自动识别命令消息。
//   - 验证失败返回清晰的错误。
type ValidationMiddleware struct {
	validator validate.IValidator
}

// NewValidationMiddleware 创建Validation中间件。
func NewValidationMiddleware(validator validate.IValidator) *ValidationMiddleware {
	return &ValidationMiddleware{
		validator: validator,
	}
}

// Handle 处理消息并执行业务处理逻辑。
//
// 说明：
// - Handle 实现 messaging.IMiddleware 接口。
// - 执行流程：
// - 1. 检查消息类型，只处理命令。
// - 2. 提取命令的 Payload
// - 3. 使用 validator 验证 Payload
// - 4. 验证通过则调用 next 继续执行链。
// - 5. 验证失败则返回错误，中断执行链。
func (m *ValidationMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	// 未配置验证器时直接透传，避免空指针
	if m == nil || m.validator == nil {
		return next(ctx, message)
	}

	// 只处理命令消息
	if message.GetKind() != messaging.KindCommand {
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
	if payload.IsNil() {
		// Payload 为空，跳过验证
		return next(ctx, message)
	}

	// 使用 validator 验证 Payload
	if err := m.validator.Validate(messaging.PayloadValue(payload)); err != nil {
		return errors.Wrap(err, errors.Validation, "command validation failed").
			WithContext("command_type", cmd.GetCommandType())
	}

	// 验证通过，继续执行
	return next(ctx, message)
}

// Name 返回名称。
//
// 说明：
// - Name 实现 messaging.IMiddleware 接口。
func (m *ValidationMiddleware) Name() string {
	return "CommandValidation"
}
