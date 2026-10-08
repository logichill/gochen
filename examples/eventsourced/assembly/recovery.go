package main

import (
	"context"
	"fmt"
	"time"

	esapp "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	"gochen/app/operation"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing/store/snapshot"
)

type resultStore struct {
	*operation.MemoryStore
	fail bool
}

func (s *resultStore) PutWithIdempotencyKey(ctx context.Context, key string, r *operation.Result) error {
	if s.fail {
		return errors.NewCode(errors.Dependency, "result storage unavailable")
	}
	return s.MemoryStore.PutWithIdempotencyKey(ctx, key, r)
}
func recoverOperationResult() {
	storage := &resultStore{operation.NewMemoryStore(), true}
	runner := operation.NewRunner(&operation.RunnerOptions{Store: storage})
	spec := &operation.Spec{Type: "credit", Mode: operation.ModeTracked, IdempotencyKey: "credit-request-1"}
	writes := 0
	result, err := runner.Execute(context.Background(), spec, func(context.Context) (*operation.Result, error) {
		writes++
		return &operation.Result{Resource: &operation.Resource{Type: "Account", ID: "1"}}, nil
	})
	check(contextx.IsAfterCommitError(err) && result != nil, "missing committed result")
	// 保留原 result 和幂等键，确认业务已成功后补存；不要释放预留或生成新 operation ID。
	recoveryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	storage.fail = false
	recovered, err := storage.RecoverResult(recoveryCtx, spec.IdempotencyKey, result)
	must(err)
	check(recovered.Operation.ID == result.Operation.ID, "recovery changed operation ID")
	freshRunner := operation.NewRunner(&operation.RunnerOptions{Store: storage})
	replayed, err := freshRunner.Execute(recoveryCtx, spec, func(context.Context) (*operation.Result, error) { writes++; return nil, nil })
	must(err)
	check(writes == 1 && replayed.Operation.ID == result.Operation.ID, "recovery re-executed business")
	fmt.Println("operation recovery: stored original result; new runner replays without execution")
}

type snapshotStore struct {
	snapshot.ISnapshotStore[int64]
	fail bool
}

func (s *snapshotStore) SaveSnapshot(ctx context.Context, ss snapshot.Snapshot[int64]) error {
	if s.fail {
		return errors.NewCode(errors.Dependency, "snapshot storage unavailable")
	}
	return s.ISnapshotStore.SaveSnapshot(ctx, ss)
}
func rebuildSnapshot() {
	s := newAssembly()
	storage, err := esstore.NewDomainEventStore(s.options(esstore.PublishModePersistOnly))
	must(err)
	repo, err := esapp.NewEventSourcedRepository(s.repositoryOptions(storage))
	must(err)
	snapshots := &snapshotStore{snapshot.NewMemoryStore[int64](), true}
	manager := snapshot.NewManager[int64](snapshots, &snapshot.Config{Enabled: true, Frequency: 1})
	decorated, err := esstore.NewSnapshottingRepository("Account", repo, esstore.SnapshottingRepositoryOptions[int64]{SnapshotManager: manager, FailOnError: true})
	must(err)
	a := s.aggregate(1)
	must(a.ApplyAndRecord(&balanceChanged{12}))
	err = decorated.Save(context.Background(), a)
	check(contextx.IsAfterCommitError(err), "expected committed snapshot failure")
	recoveryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshots.fail = false
	// 从事件事实恢复独立实例，再创建该版本的快照，不重跑余额修改或 Save。
	fresh, err := repo.Get(recoveryCtx, 1)
	must(err)
	must(manager.CreateSnapshot(recoveryCtx, 1, "Account", fresh, fresh.GetVersion()))
	stored, err := snapshots.FindSnapshot(recoveryCtx, "Account", 1)
	must(err)
	check(fresh.Balance == 12 && fresh.GetVersion() == 1 && stored.Version == 1, "snapshot recovery changed business state")
	fmt.Println("snapshot recovery: rebuild from committed events without business replay")
}
