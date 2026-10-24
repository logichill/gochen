package audited

import (
	"context"
	"testing"
	"time"

	appcrud "gochen/app/crud"
	"gochen/contextx"
	"gochen/domain/audited"
	"gochen/domain/crud"
)

// operatorCaptureRepo 记录 Update 时 ctx 中的 operator，用于验证审计归属。
type operatorCaptureRepo struct {
	*memAuditedRepo
	updateOperator string
}

func (r *operatorCaptureRepo) Update(ctx context.Context, e *auditedTestEntity) error {
	r.updateOperator = contextx.Operator(ctx)
	return r.memAuditedRepo.Update(ctx, e)
}

// P2-035 回归：Restore 必须在进入流程前把操作人注入主 ctx，
// 使下游钩子与仓储 Update 都能拿到审计归属。
func TestRestoreInjectsOperatorIntoMainContext(t *testing.T) {
	base := newMemAuditedRepo()
	repo := &operatorCaptureRepo{memAuditedRepo: base}
	store := &recordingAuditStore{}
	var beforeOperator string
	app, err := NewApplication[*auditedTestEntity, int64](repo, store, &appcrud.Options[*auditedTestEntity, int64]{
		Hooks: &appcrud.Hooks[*auditedTestEntity, int64]{
			Restore: appcrud.Stage[*auditedTestEntity]{
				Before: func(ctx context.Context, _ *auditedTestEntity) error {
					beforeOperator = contextx.Operator(ctx)
					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := e.SoftDeleteBy("alice", time.Now()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	base.items[1] = e

	if err := app.Restore(context.Background(), 1, "bob"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if beforeOperator != "bob" {
		t.Fatalf("Before hook operator = %q, want bob", beforeOperator)
	}
	if repo.updateOperator != "bob" {
		t.Fatalf("repository Update operator = %q, want bob", repo.updateOperator)
	}
}
