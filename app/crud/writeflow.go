package crud

import (
	"context"

	"gochen/app/internal/writeflow"
)

// RunBeforeCreate 执行显式 Hooks.BeforeCreate；未配置则 no-op。
func (s *Application[T, ID]) RunBeforeCreate(ctx context.Context, entity T) error {
	return s.runBeforeCreate(ctx, entity)
}

// RunAfterCreate 执行显式 Hooks.AfterCreate；未配置则 no-op。
func (s *Application[T, ID]) RunAfterCreate(ctx context.Context, entity T) error {
	return s.runAfterCreate(ctx, entity)
}

// RunBeforeUpdate 执行显式 Hooks.BeforeUpdate；未配置则 no-op。
func (s *Application[T, ID]) RunBeforeUpdate(ctx context.Context, entity T) error {
	return s.runBeforeUpdate(ctx, entity)
}

// RunAfterUpdate 执行显式 Hooks.AfterUpdate；未配置则 no-op。
func (s *Application[T, ID]) RunAfterUpdate(ctx context.Context, entity T) error {
	return s.runAfterUpdate(ctx, entity)
}

// PostCommitCreateCallback 返回 create 的提交后回调。
func (s *Application[T, ID]) PostCommitCreateCallback(entity T) func(context.Context) error {
	return s.postCommitCreate(entity)
}

// PostCommitUpdateCallback 返回 update 的提交后回调。
func (s *Application[T, ID]) PostCommitUpdateCallback(entity T) func(context.Context) error {
	return s.postCommitUpdate(entity)
}

// PostCommitDeleteCallback 返回 delete 的提交后回调。
func (s *Application[T, ID]) PostCommitDeleteCallback(id ID) func(context.Context) error {
	return s.postCommitDelete(id)
}

func (s *Application[T, ID]) runWriteFlow(ctx context.Context, plan writeflow.Plan) error {
	txRepo, _ := s.transactionalRepository()
	return writeflow.Run(ctx, txRepo, plan)
}

func callbacksToPostCommits(callbacks []func(context.Context) error) []writeflow.PostCommit {
	postCommits := make([]writeflow.PostCommit, 0, len(callbacks))
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		postCommits = append(postCommits, callback)
	}
	return postCommits
}
