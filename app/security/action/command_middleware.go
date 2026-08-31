// 命令入口的 L1 动作级 PEP。
//
// 为什么挂在 CommandExecutor 而不是 CommandBus：
// `CommandBus.Use` 注册的是**投递侧**中间件，只在本进程调用 Dispatch 时经过；
// 命令若由 transport 直接投递（跨进程、重放、死信重投），投递侧中间件根本不执行。
// `CommandExecutor.Use` 位于 handler 调用链上，是命令真正被执行前的唯一必经点，
// 因此动作校验必须挂在这里——否则就是"只拦本地调用"的假防线。
//
// 与 CRUD 装饰器的分工：本中间件只回答"这个主体能不能发起这个命令"（L1 动作级）。
// 命令内部写数据的资源级授权仍由 handler 所用的 Application 承担——
// handler 必须通过被 security.Scoped 装饰的 Application 写数据，
// 即便漏装，仓储侧的受控约束通道也会 fail-closed 兜底。

package action

import (
	"context"
	"strings"

	authaction "gochen/auth/action"

	"gochen/errors"
	"gochen/messaging"
)

// CommandPolicy 把命令类型映射到三段式动作码。
//
// Fail-Closed（§4.1）：映射表里没有的命令一律拒绝。这与 OperationPolicy
// 的空字段语义一致——"没配置"不等于"不需要鉴权"。若某类命令确实无需管控，
// 不要把它接入本中间件所在的 executor。
type CommandPolicy map[string]string

// CodeFor 返回命令类型对应的动作码。
func (p CommandPolicy) CodeFor(commandType string) (string, bool) {
	code, ok := p[strings.TrimSpace(commandType)]
	if !ok || strings.TrimSpace(code) == "" {
		return "", false
	}
	return code, true
}

// Validate 校验映射表中所有动作码合法，并拒绝空命令类型。
func (p CommandPolicy) Validate() error {
	if len(p) == 0 {
		return errors.NewCode(errors.InvalidInput, "command policy cannot be empty")
	}
	for commandType, code := range p {
		if strings.TrimSpace(commandType) == "" {
			return errors.NewCode(errors.InvalidInput, "command type cannot be empty in command policy")
		}
		if !authaction.IsValidCode(code) {
			return errors.NewCode(errors.InvalidInput, "invalid action code in command policy").
				WithContext("command_type", commandType).
				WithContext("action", code)
		}
	}
	return nil
}

// commandMiddleware 在命令执行前校验动作码。
type commandMiddleware struct {
	checker authaction.IActionChecker
	policy  CommandPolicy
}

// NewCommandMiddleware 构造命令入口的动作级 PEP。
//
// 装配期校验 checker 非空、policy 非空且动作码合法，把配置错误暴露在启动期。
// 用法：
//
//	mw, err := secaction.NewCommandMiddleware(checker, secaction.CommandPolicy{
//	    "CreateOrder": "order:api:create",
//	})
//	executor.Use(mw)
func NewCommandMiddleware(
	checker authaction.IActionChecker,
	policy CommandPolicy,
) (messaging.IMiddleware, error) {
	if checker == nil {
		return nil, errors.NewCode(errors.InvalidInput, "action checker cannot be nil")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	// 复制一份，避免调用方在运行期改表导致策略漂移。
	snapshot := make(CommandPolicy, len(policy))
	for commandType, code := range policy {
		snapshot[strings.TrimSpace(commandType)] = code
	}
	return &commandMiddleware{checker: checker, policy: snapshot}, nil
}

// Name 供中间件管道诊断使用。
func (m *commandMiddleware) Name() string { return "security.action.command" }

// Handle 校验通过后才放行到下一环。
func (m *commandMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	if message == nil {
		return errors.NewCode(errors.InvalidInput, "message is nil")
	}
	// 只管命令；同一管道上的其他消息类别原样放行，不越权拦截。
	if message.GetKind() != messaging.KindCommand {
		return next(ctx, message)
	}
	commandType := strings.TrimSpace(message.GetType())
	if commandType == "" {
		return errors.NewCode(errors.InvalidInput, "command type cannot be empty")
	}
	code, ok := m.policy.CodeFor(commandType)
	if !ok {
		return errors.NewCode(errors.Forbidden, "command is not configured in action policy").
			WithContext("command_type", commandType)
	}
	if err := authaction.RequireAction(ctx, m.checker, code); err != nil {
		return err
	}
	return next(ctx, message)
}
