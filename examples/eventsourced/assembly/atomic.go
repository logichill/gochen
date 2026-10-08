package main

import (
	"context"
	"fmt"
	esapp "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	"gochen/errors"
)

func atomicTransfer() {
	ctx := context.Background()
	s := newAssembly()
	storage, err := esstore.NewAtomicDomainEventStore(s.options(esstore.PublishModePersistOnly))
	must(err)
	repo, err := esapp.NewAtomicEventSourcedRepository(s.repositoryOptions(storage))
	must(err)
	a, b := s.aggregate(1), s.aggregate(2)
	must(a.ApplyAndRecord(&balanceChanged{100}))
	must(b.ApplyAndRecord(&balanceChanged{20}))
	must(repo.SaveAll(ctx, []*account{a, b}))

	staleB, err := repo.Get(ctx, 2)
	must(err)
	must(b.ApplyAndRecord(&balanceChanged{5}))
	must(repo.Save(ctx, b))
	must(a.ApplyAndRecord(&balanceChanged{-10}))
	must(staleB.ApplyAndRecord(&balanceChanged{10}))
	err = repo.SaveAll(ctx, []*account{a, staleB})
	check(errors.Is(err, errors.Concurrency), "stale batch must conflict")
	restored, err := repo.Get(ctx, 1)
	must(err)
	check(restored.Balance == 100 && len(a.GetUncommittedEvents()) == 1, "conflicting batch partially committed")

	// 从当前状态重新作业务决定；不要将旧待提交事件直接追加到新版本。
	a, err = repo.Get(ctx, 1)
	must(err)
	b, err = repo.Get(ctx, 2)
	must(err)
	check(a.Balance >= 10, "insufficient balance")
	must(a.ApplyAndRecord(&balanceChanged{-10}))
	must(b.ApplyAndRecord(&balanceChanged{10}))
	must(repo.SaveAll(ctx, []*account{a, b}))
	check(a.Balance == 90 && b.Balance == 35, "transfer lost value")
	fmt.Println("atomic transfer: conflict rolls back entire batch; retry uses fresh state")
}
