package crud

import (
	"context"
	"testing"

	"gochen/app"
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestProtectedUpdateRejectsInputBeforeHooks(t *testing.T) {
	for _, command := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "execute"}[command], func(t *testing.T) {
			hookCalls := 0
			application, repo := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge, Hooks: Hooks[*testEntity]{
				ActionUpdate: Stage[*testEntity]{Before: func(_ context.Context, entity *testEntity) error {
					hookCalls++
					entity.Owner = "alice"
					return nil
				}},
			}})
			ctx := context.Background()
			entity := &testEntity{Owner: "alice", Name: "original"}
			if err := application.Create(ctx, entity); err != nil {
				t.Fatal(err)
			}
			var err error
			if command {
				_, err = application.Execute(ctx, ActionUpdate, entity.ID, func(_ context.Context, candidate *testEntity) error {
					candidate.Owner = "bob"
					candidate.Name = "changed"
					return nil
				})
			} else {
				entity.Owner, entity.Name = "bob", "changed"
				err = application.Update(ctx, entity)
			}
			if errors.Code(err) != errors.Forbidden || hookCalls != 0 {
				t.Fatalf("err=%v, hooks=%d", err, hookCalls)
			}
			stored, err := repo.Get(ctx, entity.ID, spec.All())
			if err != nil {
				t.Fatal(err)
			}
			if stored.Owner != "alice" || stored.Name != "original" {
				t.Fatalf("stored=%+v", stored)
			}
		})
	}
}

type failingAuthorizer struct{ err error }

func (a failingAuthorizer) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }
func (a failingAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	return auth.Decision{}, a.err
}

func TestAuthorizationPreservesFaultCodes(t *testing.T) {
	for _, code := range []errors.ErrorCode{errors.Internal, errors.Database, errors.Unauthorized, errors.Forbidden} {
		application, _ := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge, Authorizer: failingAuthorizer{errors.NewCode(code, "authorization failure")}})
		_, err := application.Get(context.Background(), 1)
		if errors.Code(err) != code {
			t.Fatalf("err=%v, want=%v", err, code)
		}
	}
}

func TestResourceContractsBindIndependentlyAndRejectMutableAuthorization(t *testing.T) {
	protected := newTestRepo()
	contract := app.ResourceContract{Mutable: []meta.Dimension{"owner"}}
	mutable, err := testkit.NewMemoryRepository[*testEntity, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*testEntity, int64](contract))
	if err != nil {
		t.Fatal(err)
	}
	// 组合根修改原声明时，已构造的契约保持冻结。
	contract.Mutable[0] = "missing"
	for _, reference := range []meta.Ref{meta.ByDimension("owner"), meta.ByField("Owner")} {
		_, err = NewApplication(mutable, Options[*testEntity, int64]{DeleteMode: DeletePurge, Resource: "mutable", Authorizer: fixedAuthorizer{vocabulary: auth.Vocabulary{Refs: []meta.Ref{reference}}}})
		if errors.Code(err) != errors.InvalidInput {
			t.Fatalf("mutable reference %v: %v", reference, err)
		}
	}
	for _, test := range []struct {
		repo *testkit.MemoryRepository[*testEntity, int64]
		want errors.ErrorCode
	}{{protected, errors.Forbidden}, {mutable, ""}} {
		application, err := NewApplication(test.repo, Options[*testEntity, int64]{DeleteMode: DeletePurge, Resource: "document", Authorizer: auth.Allow()})
		if err != nil {
			t.Fatal(err)
		}
		entity := &testEntity{Owner: "alice", Name: "original"}
		if err := application.Create(context.Background(), entity); err != nil {
			t.Fatal(err)
		}
		entity.Owner = "bob"
		err = application.Update(context.Background(), entity)
		if errors.Code(err) != test.want {
			t.Fatalf("update=%v, want=%v", err, test.want)
		}
	}
	_, err = testkit.NewMemoryRepository[*testEntity, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*testEntity, int64](app.ResourceContract{Transferable: []meta.Dimension{"missing"}}))
	if errors.Code(err) != errors.InvalidInput {
		t.Fatalf("ghost dimension=%v", err)
	}
}
