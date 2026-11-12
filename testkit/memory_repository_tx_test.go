package testkit

import (
	"context"
	"testing"

	audited "gochen/audit"
	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

// P2#17 特征测试（与 gochen-runtime db/orm/repo/transaction.go 对齐）：
// 嵌套内层回滚为 no-op，内层写入随外层提交固化（生产 SQL 嵌套事务同样
// 不撤销已执行写入）；外层与其后提交照常生效。
func TestNestedMemoryTxInnerRollbackKeepsInnerWritesRuntimeParity(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	if err := r.Create(context.Background(), &stringMemoryEntity{ID: "a", Name: "outer"}, spec.All()); err != nil {
		t.Fatal(err)
	}
	err := r.WithinTx(context.Background(), func(outerCtx context.Context) error {
		if err := r.Create(outerCtx, &stringMemoryEntity{ID: "b", Name: "outer-direct"}, spec.All()); err != nil {
			return err
		}
		innerErr := r.WithinTx(outerCtx, func(innerCtx context.Context) error {
			if err := r.Create(innerCtx, &stringMemoryEntity{ID: "c", Name: "nested"}, spec.All()); err != nil {
				return err
			}
			return context.Canceled // 触发内层回滚（no-op）
		})
		if innerErr == nil {
			t.Fatal("expected inner tx error")
		}
		return nil // 外层提交应成功
	})
	if err != nil {
		t.Fatalf("outer tx must commit after inner rollback, got %v", err)
	}
	// 内层写入不因内层回滚被撤销（与生产 SQL 嵌套语义一致）
	if ok, _ := r.Exists(context.Background(), "c", spec.All()); !ok {
		t.Fatal("inner write must survive inner rollback (runtime parity)")
	}
	// 外层在进入内层前的写入必须保留
	if ok, _ := r.Exists(context.Background(), "b", spec.All()); !ok {
		t.Fatal("outer write lost after inner rollback")
	}
	if e, err := r.Get(context.Background(), "b", spec.All()); err != nil || e.Name != "outer-direct" {
		t.Fatalf("outer write value lost: entity=%+v err=%v", e, err)
	}
}

// 外层自身回滚不受 aborted 标记影响（回归保护）。
func TestMemoryTxOuterRollbackStillWorks(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	if err := r.Create(context.Background(), &stringMemoryEntity{ID: "a"}, spec.All()); err != nil {
		t.Fatal(err)
	}
	if err := r.WithinTx(context.Background(), func(ctx context.Context) error {
		if err := r.Create(ctx, &stringMemoryEntity{ID: "b"}, spec.All()); err != nil {
			return err
		}
		return errRollback
	}); err == nil {
		t.Fatal("expected rollback")
	}
	if ok, _ := r.Exists(context.Background(), "b", spec.All()); ok {
		t.Fatal("write survived outer rollback")
	}
	if ok, _ := r.Exists(context.Background(), "a", spec.All()); !ok {
		t.Fatal("pre-tx entity lost")
	}
}

// 无嵌套场景的提交不受影响（回归保护）。
func TestMemoryTxPlainCommitStillWorks(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	if err := r.WithinTx(context.Background(), func(ctx context.Context) error {
		return r.Create(ctx, &stringMemoryEntity{ID: "x"}, spec.All())
	}); err != nil {
		t.Fatalf("plain tx: %v", err)
	}
	if ok, _ := r.Exists(context.Background(), "x", spec.All()); !ok {
		t.Fatal("plain tx write lost")
	}
}

// P1-014 回归：嵌套内层回滚后，内层注册的 after-commit 回调不得执行，
// 外层回调仍随外层提交正常执行（保存点语义）。
func TestNestedRollbackPreventsAfterCommitCallbacks(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	outerRan := false
	innerRan := false
	err := r.WithinTx(context.Background(), func(outerCtx context.Context) error {
		if err := contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			outerRan = true
			return nil
		}); err != nil {
			return err
		}
		innerErr := r.WithinTx(outerCtx, func(innerCtx context.Context) error {
			if err := contextx.AppendAfterCommit(innerCtx, func(context.Context) error {
				innerRan = true
				return nil
			}); err != nil {
				return err
			}
			return context.Canceled
		})
		if innerErr == nil {
			t.Fatal("expected inner tx error")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer commit must succeed after inner savepoint rollback, got %v", err)
	}
	if innerRan {
		t.Fatal("after-commit callback registered in rolled-back inner scope must not run")
	}
	if !outerRan {
		t.Fatal("outer after-commit callback must run on outer commit")
	}
}

var errRollback = errors.NewCode(errors.FailedPrecondition, "rollback requested")

// 多层嵌套（P2#17 特征测试）：任意内层回滚均为 no-op，全部写入随最外层提交固化。
func TestNestedMemoryTxMultiLevelInnerRollbackNoOpRuntimeParity(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	err := r.WithinTx(context.Background(), func(l1 context.Context) error {
		if err := r.Create(l1, &stringMemoryEntity{ID: "l1", Name: "level1"}, spec.All()); err != nil {
			return err
		}
		return r.WithinTx(l1, func(l2 context.Context) error {
			if err := r.Create(l2, &stringMemoryEntity{ID: "l2", Name: "level2"}, spec.All()); err != nil {
				return err
			}
			innerErr := r.WithinTx(l2, func(l3 context.Context) error {
				if err := r.Create(l3, &stringMemoryEntity{ID: "l3", Name: "level3-dirty"}, spec.All()); err != nil {
					return err
				}
				return context.Canceled
			})
			if innerErr == nil {
				t.Fatal("expected level-3 rollback")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("outer commit: %v", err)
	}
	for _, id := range []string{"l1", "l2", "l3"} {
		if ok, _ := r.Exists(context.Background(), id, spec.All()); !ok {
			t.Fatalf("write %q lost after inner rollback (runtime parity)", id)
		}
	}
}

// 内层提交时其写入并入外层，外层回滚后一并丢弃。
func TestNestedMemoryTxInnerCommitMergesIntoOuter(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	err := r.WithinTx(context.Background(), func(outer context.Context) error {
		innerErr := r.WithinTx(outer, func(inner context.Context) error {
			return r.Create(inner, &stringMemoryEntity{ID: "merged", Name: "inner"}, spec.All())
		})
		if innerErr != nil {
			return innerErr
		}
		return errRollback // 外层回滚
	})
	if err == nil {
		t.Fatal("expected outer rollback")
	}
	if ok, _ := r.Exists(context.Background(), "merged", spec.All()); ok {
		t.Fatal("inner-committed write must be discarded when outer rolls back")
	}
}

// 同一嵌套作用域的重复 Commit 必须幂等，不得误释放外层保存点。
func TestNestedMemoryTxRepeatedCommitKeepsOuterSavepoint(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	err := r.WithinTx(context.Background(), func(outerCtx context.Context) error {
		l1, err := r.BeginTx(outerCtx)
		if err != nil {
			return err
		}
		if err := r.Create(l1.Context(), &stringMemoryEntity{ID: "l1"}, spec.All()); err != nil {
			return err
		}
		l2, err := r.BeginTx(l1.Context())
		if err != nil {
			return err
		}
		if err := r.Create(l2.Context(), &stringMemoryEntity{ID: "l2"}, spec.All()); err != nil {
			return err
		}
		if err := r.Commit(l2); err != nil {
			return err
		}
		if err := r.Commit(l2); err != nil {
			return err
		}
		return r.Rollback(l1)
	})
	if err != nil {
		t.Fatalf("outer commit after nested rollback must succeed, got %v", err)
	}
	for _, id := range []string{"l1", "l2"} {
		if ok, _ := r.Exists(context.Background(), id, spec.All()); !ok {
			t.Fatalf("nested write %q lost (runtime parity)", id)
		}
	}
}

// 同一嵌套作用域的重复 Rollback 必须幂等（no-op），写入随外层提交固化。
func TestNestedMemoryTxRepeatedRollbackIsIdempotent(t *testing.T) {
	r, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	err := r.WithinTx(context.Background(), func(outerCtx context.Context) error {
		l1, err := r.BeginTx(outerCtx)
		if err != nil {
			return err
		}
		if err := r.Create(l1.Context(), &stringMemoryEntity{ID: "l1"}, spec.All()); err != nil {
			return err
		}
		if err := r.Rollback(l1); err != nil {
			return err
		}
		return r.Rollback(l1)
	})
	if err != nil {
		t.Fatalf("outer commit after repeated nested rollback must succeed, got %v", err)
	}
	if ok, _ := r.Exists(context.Background(), "l1", spec.All()); !ok {
		t.Fatal("nested write must survive nested rollback (runtime parity)")
	}
}

// 审计仓储：嵌套作用域重复 Commit 幂等（no-op），内层回滚同样 no-op，
// 实体与审计记录随外层提交固化（与生产 SQL 嵌套语义一致）。
func TestAuditedNestedTxRepeatedCommitIsIdempotentNoOp(t *testing.T) {
	r, repositoryErr := NewAuditedMemoryRepository[*auditedMemoryEntity, int64](NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	err := r.WithinTx(ctx, func(outer context.Context) error {
		l1, err := r.BeginTx(outer)
		if err != nil {
			return err
		}
		if _, err := r.SaveAuditRecord(l1.Context(), audited.AuditRecord{
			EntityID: "1",
			Action:   "create",
			Operator: "l1", OperationID: "test-operation", ResourceKind: "doc",
		}); err != nil {
			return err
		}
		l2, err := r.BeginTx(l1.Context())
		if err != nil {
			return err
		}
		if _, err := r.SaveAuditRecord(l2.Context(), audited.AuditRecord{
			EntityID: "1",
			Action:   "update",
			Operator: "l2", OperationID: "test-operation", ResourceKind: "doc",
		}); err != nil {
			return err
		}
		if err := r.Commit(l2); err != nil {
			return err
		}
		if err := r.Commit(l2); err != nil {
			return err
		}
		return r.Rollback(l1)
	})
	if err != nil {
		t.Fatalf("outer commit after nested rollback must succeed, got %v", err)
	}

	records, err := r.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if err != nil {
		t.Fatalf("list audit records: %v", err)
	}
	if len(records) != 2 || records[0].Operator != "l1" || records[1].Operator != "l2" {
		t.Fatalf("nested audit records must survive nested rollback (runtime parity): %+v", records)
	}
}

// P2#17 特征测试：审计仓储嵌套内层回滚为 no-op，内层审计记录随外层提交固化。
func TestAuditedNestedTxInnerRollbackKeepsInnerAuditRuntimeParity(t *testing.T) {
	r, repositoryErr := NewAuditedMemoryRepository[*auditedMemoryEntity, int64](NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	err := r.WithinTx(ctx, func(outer context.Context) error {
		if _, err := r.SaveAuditRecord(outer, audited.AuditRecord{
			EntityID: "1",
			Action:   "create",
			Operator: "outer", OperationID: "test-operation", ResourceKind: "doc",
		}); err != nil {
			return err
		}
		innerErr := r.WithinTx(outer, func(inner context.Context) error {
			if _, err := r.SaveAuditRecord(inner, audited.AuditRecord{
				EntityID: "1",
				Action:   "delete",
				Operator: "inner", OperationID: "test-operation", ResourceKind: "doc",
			}); err != nil {
				return err
			}
			return context.Canceled
		})
		if innerErr == nil {
			t.Fatal("expected inner rollback")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer commit: %v", err)
	}

	records, err := r.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if err != nil {
		t.Fatalf("list audit records: %v", err)
	}
	if len(records) != 2 || records[0].Operator != "outer" || records[1].Operator != "inner" {
		t.Fatalf("inner audit record must survive inner rollback (runtime parity): %+v", records)
	}
}

// errInnerResource = 内层资源事务请求回滚的错误。
var errInnerResource = errors.NewCode(errors.FailedPrecondition, "inner resource rollback requested")

// P2#16 回归：内存仓储事务作为独立资源事务嵌套在宿主事务链路（另一仓储的
// WithinTx）内时，其提交不得封存宿主共享的 after-commit 回调批次——宿主在
// 资源事务提交后仍可注册回调，且全部回调随宿主提交正常执行。
func TestChainNestedResourceTxCommitDoesNotSealHostCallbacks(t *testing.T) {
	host, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	inner, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	hostCbRan, innerCbRan, hostLateCbRan := false, false, false
	err := host.WithinTx(ctx, func(outerCtx context.Context) error {
		if err := contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			hostCbRan = true
			return nil
		}); err != nil {
			return err
		}
		if err := inner.WithinTx(outerCtx, func(innerCtx context.Context) error {
			if err := contextx.AppendAfterCommit(innerCtx, func(context.Context) error {
				innerCbRan = true
				return nil
			}); err != nil {
				return err
			}
			return inner.Create(innerCtx, &stringMemoryEntity{ID: "inner"}, spec.All())
		}); err != nil {
			return err
		}
		// 资源事务提交后宿主仍可注册回调（回归：旧实现已把共享批次封存，
		// 此处会报 "transaction already committed"）。
		return contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			hostLateCbRan = true
			return nil
		})
	})
	if err != nil {
		t.Fatalf("host tx must succeed: %v", err)
	}
	if !hostCbRan || !innerCbRan || !hostLateCbRan {
		t.Fatalf("all after-commit callbacks must run on host commit: host=%v inner=%v late=%v", hostCbRan, innerCbRan, hostLateCbRan)
	}
	if ok, _ := inner.Exists(ctx, "inner", spec.All()); !ok {
		t.Fatal("inner resource tx data must be committed")
	}
}

// P2#16 回归：链路内独立资源事务回滚不得清空/封存宿主已注册的回调——宿主
// 回调随宿主提交正常执行，仅内层新增回调被撤销；宿主在内层失败后仍可继续
// 注册回调并提交。
func TestChainNestedResourceTxRollbackKeepsHostCallbacks(t *testing.T) {
	host, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	inner, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	hostCbRan, innerCbRan, hostLateCbRan := false, false, false
	err := host.WithinTx(ctx, func(outerCtx context.Context) error {
		if err := contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			hostCbRan = true
			return nil
		}); err != nil {
			return err
		}
		innerErr := inner.WithinTx(outerCtx, func(innerCtx context.Context) error {
			if err := contextx.AppendAfterCommit(innerCtx, func(context.Context) error {
				innerCbRan = true
				return nil
			}); err != nil {
				return err
			}
			if err := inner.Create(innerCtx, &stringMemoryEntity{ID: "inner"}, spec.All()); err != nil {
				return err
			}
			return errInnerResource
		})
		if innerErr == nil {
			t.Fatal("expected inner resource tx failure")
		}
		// 内层回滚后宿主仍可注册回调并提交（回归：旧实现清空并封死了宿主状态）。
		return contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			hostLateCbRan = true
			return nil
		})
	})
	if err != nil {
		t.Fatalf("host tx must succeed after recovering from inner failure: %v", err)
	}
	if !hostCbRan || !hostLateCbRan {
		t.Fatalf("host callbacks must run on host commit: host=%v late=%v", hostCbRan, hostLateCbRan)
	}
	if innerCbRan {
		t.Fatal("after-commit callback registered in rolled-back inner resource tx must not run")
	}
	if ok, _ := inner.Exists(ctx, "inner", spec.All()); ok {
		t.Fatal("inner resource tx data must be rolled back")
	}
}

// P2#16 回归：审计仓储事务作为链路内独立资源事务时同样不得触碰宿主回调状态。
func TestAuditedChainNestedResourceTxKeepsHostCallbacks(t *testing.T) {
	host, repositoryErr := NewMemoryRepository[*stringMemoryEntity, string](nil)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	inner, repositoryErr := NewAuditedMemoryRepository[*auditedMemoryEntity, int64](NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	hostCbRan, innerCbRan := false, false
	err := host.WithinTx(ctx, func(outerCtx context.Context) error {
		if err := contextx.AppendAfterCommit(outerCtx, func(context.Context) error {
			hostCbRan = true
			return nil
		}); err != nil {
			return err
		}
		if err := inner.WithinTx(outerCtx, func(innerCtx context.Context) error {
			if err := contextx.AppendAfterCommit(innerCtx, func(context.Context) error {
				innerCbRan = true
				return nil
			}); err != nil {
				return err
			}
			if _, err := inner.SaveAuditRecord(innerCtx, audited.AuditRecord{
				EntityID: "1",
				Action:   "create",
				Operator: "inner", OperationID: "test-operation", ResourceKind: "doc",
			}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		return contextx.AppendAfterCommit(outerCtx, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatalf("host tx must succeed: %v", err)
	}
	if !hostCbRan || !innerCbRan {
		t.Fatalf("all after-commit callbacks must run on host commit: host=%v inner=%v", hostCbRan, innerCbRan)
	}
	records, listErr := inner.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if listErr != nil {
		t.Fatalf("list audit records: %v", listErr)
	}
	if len(records) != 1 {
		t.Fatalf("inner audited record must be committed: %+v", records)
	}
}
