package command

import (
	"context"
	"fmt"
	"gochen/contextx"
	"gochen/errors"
	"gochen/messaging"
	"runtime/debug"
	"strings"
	"sync"
)

// ICommandExecutor 抽象“同步命令执行”端口。
//
// 语义约定：
// - Execute 返回真实命令处理结果；
// - 该接口不表达“消息已进入 transport 即成功”的异步投递语义；
// - 适合 saga / process 这类需要依赖执行结果推进流程的编排层。
type ICommandExecutor interface {
	Execute(ctx context.Context, cmd *Command) error
}

// CommandExecutor 是本地命令执行器。
//
// 它持有本地 handler registry，并在当前调用栈内执行命令与执行侧中间件。
type CommandExecutor struct {
	mu          sync.RWMutex
	handlers    map[string]CommandHandlerFunc
	middlewares []messaging.IMiddleware
}

// NewCommandExecutor 创建本地命令执行器。
func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{
		handlers: make(map[string]CommandHandlerFunc),
	}
}

// RegisterHandler 注册命令处理器；重复注册会直接覆盖旧处理器。
func (e *CommandExecutor) RegisterHandler(commandType string, handler CommandHandlerFunc) error {
	if e == nil {
		return errors.NewCode(errors.InvalidInput, "command executor is nil")
	}
	commandType = strings.TrimSpace(commandType)
	if commandType == "" {
		return errors.NewCode(errors.InvalidInput, "command type cannot be empty")
	}
	if handler == nil {
		return errors.NewCode(errors.InvalidInput, "handler cannot be nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handlers == nil {
		e.handlers = make(map[string]CommandHandlerFunc)
	}
	e.handlers[commandType] = handler
	return nil
}

// HasHandler 检查某个命令类型是否已注册处理器。
func (e *CommandExecutor) HasHandler(commandType string) bool {
	if e == nil {
		return false
	}
	commandType = strings.TrimSpace(commandType)
	if commandType == "" {
		return false
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.handlers[commandType]
	return ok
}

// Use 注册执行侧中间件。
func (e *CommandExecutor) Use(middleware messaging.IMiddleware) {
	if e == nil || middleware == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.middlewares = append(e.middlewares, middleware)
}

// Execute 同步执行命令并返回真实处理结果。
func (e *CommandExecutor) Execute(ctx context.Context, cmd *Command) error {
	if e == nil {
		return errors.NewCode(errors.InvalidInput, "command executor is nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if cmd == nil {
		return errors.NewCode(errors.InvalidInput, "command cannot be nil")
	}

	runCtx, err := deriveCommandContext(ctx, cmd)
	if err != nil {
		return err
	}

	finalHandler := func(runCtx context.Context, message messaging.IMessage) error {
		routedCmd, commandType, err := normalizeCommandMessage(message)
		if err != nil {
			return err
		}

		handler, ok := e.lookupHandler(commandType)
		if !ok {
			return errors.NewCode(errors.NotFound, "command handler not found").
				WithContext("command_type", commandType)
		}

		return invokeCommandHandler(runCtx, routedCmd, commandType, handler)
	}

	middlewares := e.middlewaresSnapshot()
	if len(middlewares) == 0 {
		return finalHandler(runCtx, cmd)
	}

	next := finalHandler
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		currentNext := next
		next = func(runCtx context.Context, message messaging.IMessage) error {
			return middleware.Handle(runCtx, message, currentNext)
		}
	}
	return next(runCtx, cmd)
}

func (e *CommandExecutor) lookupHandler(commandType string) (CommandHandlerFunc, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	handler, ok := e.handlers[commandType]
	return handler, ok
}

func (e *CommandExecutor) middlewaresSnapshot() []messaging.IMiddleware {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]messaging.IMiddleware(nil), e.middlewares...)
}

func deriveCommandContext(ctx context.Context, cmd *Command) (context.Context, error) {
	md := cmd.GetMetadata()
	derived, err := contextx.DeriveFromMetadata(ctx, md)
	if err != nil {
		return nil, err
	}
	derived, err = contextx.EnsureTraceID(derived, md, cmd.GetID())
	if err != nil {
		return nil, err
	}
	if err := contextx.InjectTenantID(derived, md); err != nil {
		return nil, err
	}
	if err := contextx.InjectOperator(derived, md); err != nil {
		return nil, err
	}
	return derived, nil
}

func normalizeCommandMessage(message messaging.IMessage) (*Command, string, error) {
	if message == nil {
		return nil, "", errors.NewCode(errors.InvalidInput, "message is nil")
	}
	if message.GetKind() != messaging.KindCommand {
		return nil, "", errors.NewCode(errors.InvalidInput, "expected command kind").
			WithContext("message_kind", string(message.GetKind())).
			WithContext("message_type", message.GetType())
	}

	cmd, ok := message.(*Command)
	if !ok {
		return nil, "", errors.NewCode(errors.InvalidInput, "expected *Command").
			WithContext("message_go_type", fmt.Sprintf("%T", message)).
			WithContext("message_type", message.GetType())
	}

	commandType := strings.TrimSpace(cmd.GetCommandType())
	if commandType == "" {
		return nil, "", errors.NewCode(errors.InvalidInput, "command type cannot be empty")
	}
	return cmd, commandType, nil
}

func invokeCommandHandler(ctx context.Context, cmd *Command, commandType string, handler CommandHandlerFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.NewCode(errors.Internal, "command handler panicked").
				WithContext("panic", fmt.Sprint(r)).
				WithContext("stack", string(debug.Stack())).
				WithContext("command_type", commandType).
				WithContext("command_id", cmd.GetID())
		}
	}()
	return handler(ctx, cmd)
}

var _ ICommandExecutor = (*CommandExecutor)(nil)
