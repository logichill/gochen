package action_test

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/app/security/action"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
)

// 命令入口 L1 PEP 的回归。
//
// 重点验证三件事：动作码正确映射、未配置的命令 fail-closed、
// 以及拒绝时**不得触达 handler**（放行才算数，拦截必须真的拦住）。

type recordingCommandChecker struct {
	seen    []string
	granted map[string]bool
}

func (c *recordingCommandChecker) RequireAction(_ context.Context, code string) error {
	c.seen = append(c.seen, code)
	if c.granted[code] {
		return nil
	}
	return errors.NewCode(errors.Forbidden, "denied").WithContext("action", code)
}

func newExecutor(t *testing.T, mw messaging.IMiddleware, invoked *bool) *command.CommandExecutor {
	t.Helper()
	executor := command.NewCommandExecutor()
	require.NoError(t, executor.RegisterHandler("CreateOrder", func(context.Context, *command.Command) error {
		*invoked = true
		return nil
	}))
	require.NoError(t, executor.RegisterHandler("DropDatabase", func(context.Context, *command.Command) error {
		*invoked = true
		return nil
	}))
	executor.Use(mw)
	return executor
}

func newCommand(commandType string) *command.Command {
	return command.NewCommand("cmd-1", commandType, "agg-1", "order", nil)
}

func policy() action.CommandPolicy {
	return action.CommandPolicy{"CreateOrder": "order:api:create"}
}

func TestCommandMiddlewareAllowsGrantedCommand(t *testing.T) {
	checker := &recordingCommandChecker{granted: map[string]bool{"order:api:create": true}}
	mw, err := action.NewCommandMiddleware(checker, policy())
	require.NoError(t, err)

	invoked := false
	executor := newExecutor(t, mw, &invoked)

	require.NoError(t, executor.Execute(context.Background(), newCommand("CreateOrder")))
	require.True(t, invoked)
	require.Equal(t, []string{"order:api:create"}, checker.seen)
}

func TestCommandMiddlewareDeniesUngrantedCommand(t *testing.T) {
	checker := &recordingCommandChecker{granted: map[string]bool{}}
	mw, err := action.NewCommandMiddleware(checker, policy())
	require.NoError(t, err)

	invoked := false
	executor := newExecutor(t, mw, &invoked)

	err = executor.Execute(context.Background(), newCommand("CreateOrder"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	// 拦截必须发生在 handler 之前。
	require.False(t, invoked)
}

// 策略里没有的命令一律拒绝——"没配置"不等于"不需要鉴权"。
func TestCommandMiddlewareFailsClosedForUnmappedCommand(t *testing.T) {
	checker := &recordingCommandChecker{granted: map[string]bool{"order:api:create": true}}
	mw, err := action.NewCommandMiddleware(checker, policy())
	require.NoError(t, err)

	invoked := false
	executor := newExecutor(t, mw, &invoked)

	err = executor.Execute(context.Background(), newCommand("DropDatabase"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, invoked)
}

// 装配期校验：动作码拼错、策略为空、checker 为空都在启动期暴露。
func TestNewCommandMiddlewareValidatesWiring(t *testing.T) {
	_, err := action.NewCommandMiddleware(nil, policy())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	checker := &recordingCommandChecker{}
	_, err = action.NewCommandMiddleware(checker, action.CommandPolicy{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = action.NewCommandMiddleware(checker, action.CommandPolicy{"CreateOrder": "order:create"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 运行期改动传入的 map 不得影响已装配的策略。
func TestCommandMiddlewareSnapshotsPolicy(t *testing.T) {
	checker := &recordingCommandChecker{granted: map[string]bool{"order:api:create": true}}
	mutable := policy()
	mw, err := action.NewCommandMiddleware(checker, mutable)
	require.NoError(t, err)

	mutable["DropDatabase"] = "db:api:drop"

	invoked := false
	executor := newExecutor(t, mw, &invoked)
	err = executor.Execute(context.Background(), newCommand("DropDatabase"))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, invoked)
}
