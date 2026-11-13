package crud_test

import (
	"context"
	"encoding/json"
	"gochen/app/operation"
	audited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/contracttest"
	"reflect"
	"testing"
)

type transferPolicy struct {
	requests    []auth.Request
	denyReceive bool
}

func (p *transferPolicy) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department"), meta.ByDimension("region")}, Assignable: []meta.Dimension{"department", "region"}}
}
func (p *transferPolicy) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	if req.Action == "move" || req.Action == "receive" {
		if _, active := contextx.TxLifecycleFromContext(ctx); active {
			return auth.Decision{}, errors.NewCode(errors.Internal, "authorization occurred inside transaction")
		}
		copyReq := req
		copyReq.Targets = append([]auth.Assignment(nil), req.Targets...)
		p.requests = append(p.requests, copyReq)
		if req.Action == "move" {
			if len(req.Targets) > 0 {
				req.Targets[0].Value = int64(999)
			}
			return auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("department"), 1)}, nil
		}
		if p.denyReceive {
			return auth.Decision{Reason: "receive denied"}, nil
		}
		// 接收方完成目标验证后输出权威赋值，客户端意图仅供判定参考。
		return auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("department"), 2), Assign: []auth.Assignment{{Dimension: "department", Value: 2}, {Dimension: "region", Value: "north"}}}, nil
	}
	return auth.Decision{Allowed: true, Spec: spec.All()}, nil
}

func TestTransferTwoPhaseAuditAndOperationIdentity(t *testing.T) {
	repo, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1),
		testkit.WithMemoryResourceContract[*contracttest.Document, int64](contracttest.Contract()),
		testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
	if err != nil {
		t.Fatal(err)
	}
	policy := &transferPolicy{}
	app, err := newTestApplication(repo, repo, testOptions[*contracttest.Document, int64]{Resource: "document", Authorizer: policy,
		Actions: []auth.Action{{Name: "move", Kind: auth.KindTransfer, ReceiveAction: "receive", Dimensions: []meta.Dimension{"department", "region"}}, {Name: "receive", Kind: auth.KindTransfer}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := contextx.WithTenantID(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = contextx.WithOperator(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	entity := &contracttest.Document{Department: 1, Name: "original"}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	ctx = operation.WithOperationID(ctx, "move-operation")
	target := int64(2)
	targets := []auth.Assignment{{Dimension: "department", Value: &target}}
	err = repo.WithinTx(ctx, func(txCtx context.Context) error {
		_, err := app.Transfer(txCtx, "move", entity.ID, entity.Version, targets)
		return err
	})
	if errors.Code(err) != errors.FailedPrecondition || len(policy.requests) != 0 {
		t.Fatalf("nested transfer: %v, requests=%v", err, policy.requests)
	}
	policy.denyReceive = true
	if _, err := app.Transfer(ctx, "move", entity.ID, entity.Version, targets); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("receive denial: %v", err)
	}
	stored, err := repo.Get(ctx, entity.ID, spec.All())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Department != 1 {
		t.Fatal("denial wrote entity")
	}
	policy.denyReceive = false
	policy.requests = nil
	after, err := app.Transfer(ctx, "move", entity.ID, entity.Version, targets)
	if err != nil {
		t.Fatal(err)
	}
	if target != 2 || after.Department != 2 || after.Region == nil || *after.Region != "north" {
		t.Fatalf("target/state: %v %+v", target, after)
	}
	if len(policy.requests) != 2 || !reflect.DeepEqual(policy.requests[0].Targets, policy.requests[1].Targets) || policy.requests[0].Targets[0].Value != int64(2) {
		t.Fatalf("frozen requests: %+v", policy.requests)
	}
	readCtx := audited.WithAuditResourceKind(ctx, "document")
	for _, side := range []struct {
		department int64
		action     string
		count      int
	}{{1, "transfer_out", 2}, {2, "transfer_in", 1}} {
		records, err := repo.ListAuditRecords(readCtx, "1", 0, 10, spec.Eq(meta.ByDimension("department"), side.department))
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != side.count {
			t.Fatalf("side %d: %+v", side.department, records)
		}
		found := false
		for _, record := range records {
			if record.Action == side.action {
				found = true
				if record.OperationID != "move-operation" {
					t.Fatalf("operation: %+v", record)
				}
				var changes []audited.FieldChange
				if err := json.Unmarshal(record.Changes, &changes); err != nil {
					t.Fatal(err)
				}
				if len(changes) != 2 {
					t.Fatalf("dimension changes: %s", record.Changes)
				}
			} else if record.Action == "transfer_in" || record.Action == "transfer_out" {
				t.Fatal("opposite side leaked")
			}
		}
		if !found {
			t.Fatalf("missing %s", side.action)
		}
	}
	_, err = app.Transfer(ctx, "move", entity.ID, after.Version, targets)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("batch duplicate: %v", err)
	}
}
