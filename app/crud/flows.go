package crud

import (
	"context"

	"gochen/app/internal/writeflow"
	"gochen/domain"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

// flows 保存构造期预装配的写流程；Flow 构造后只读，可并发复用。
type flows[T domain.IEntity[ID], ID comparable] struct {
	create    *writeflow.Flow[T]
	update    *writeflow.Flow[T]
	delete    *writeflow.Flow[ID]
	createAll *writeflow.Flow[[]T]
	updateAll *writeflow.Flow[[]T]
	deleteAll *writeflow.Flow[[]ID]
}

// Flow 将当前阶段钩子与核心动作装配为执行流。
func (s Stage[P]) Flow(tx writeflow.ITxRunner, action func(context.Context, P) error, validator func(P) error) *writeflow.Flow[P] {
	f := writeflow.New[P](tx).
		Action(action).
		Before(s.Before).
		After(s.After).
		PostCommit(s.PostCommit)
	if validator != nil {
		f.Validate(validator)
	}
	return f
}

// BatchFlow 将当前单条阶段钩子适配为批量执行流（Before/Validate 在事务外逐条预检，PostCommit 全部执行后合并错误）。
func (s Stage[P]) BatchFlow(tx writeflow.ITxRunner, action func(context.Context, []P) error, validator func(P) error) *writeflow.Flow[[]P] {
	f := writeflow.New[[]P](tx).
		Action(action).
		Before(writeflow.Each(s.Before)).
		After(writeflow.Each(s.After)).
		PostCommit(writeflow.EachAll(s.PostCommit)).
		OutsideTx()
	if validator != nil {
		f.Validate(writeflow.EachPure(validator))
	}
	return f
}

func (s *Application[T, ID]) buildFlows() flows[T, ID] {
	tx := s.txRunner()
	hasTx := tx != nil
	batchRepo, _ := s.repository.(domaincrud.IBatchOperations[T, ID])
	var createAll, updateAll func(context.Context, []T) error
	var deleteAll func(context.Context, []ID) error
	if batchRepo != nil {
		createAll = batchRepo.CreateAll
		updateAll = batchRepo.UpdateAll
		deleteAll = batchRepo.DeleteAll
	}
	return flows[T, ID]{
		create:    s.hooks.Create.Flow(tx, s.repository.Create, s.Validate),
		update:    s.hooks.Update.Flow(tx, s.repository.Update, s.Validate),
		delete:    s.hooks.Delete.Flow(tx, s.repository.Delete, nil),
		createAll: s.hooks.Create.BatchFlow(tx, resolveBatchAction("create", hasTx, createAll, s.repository.Create), s.Validate),
		updateAll: s.hooks.Update.BatchFlow(tx, resolveBatchAction("update", hasTx, updateAll, s.repository.Update), s.Validate),
		deleteAll: s.hooks.Delete.BatchFlow(tx, resolveBatchAction("delete", hasTx, deleteAll, s.repository.Delete), nil),
	}
}

func (s *Application[T, ID]) txRunner() writeflow.ITxRunner {
	txRepo, _ := s.repository.(ITransactional)
	return txRepo
}

// resolveBatchAction 解析批量核心动作：优先使用仓储原生批量接口，否则在具备事务能力时退化为逐条执行。
func resolveBatchAction[P any](
	op string,
	hasTx bool,
	batchFn func(context.Context, []P) error,
	singleFn func(context.Context, P) error,
) func(context.Context, []P) error {
	if batchFn != nil {
		return batchFn
	}
	if !hasTx {
		msg := "batch " + op + " requires repository to implement IBatchOperations or ITransactional"
		return func(context.Context, []P) error {
			return errors.NewCode(errors.Unsupported, msg)
		}
	}
	return writeflow.Each(singleFn)
}
