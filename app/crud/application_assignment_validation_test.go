package crud

import (
	"context"
	"strconv"
	"testing"

	"gochen/app"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type creationAssignedEntity struct {
	domcrud.Entity[int64]
	Owner string `dim:"owner"`
	Name  string
}

type creationAssignmentPolicy struct{ requests []auth.Request }

func (p *creationAssignmentPolicy) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Assignable: []meta.Dimension{"owner"}}
}
func (p *creationAssignmentPolicy) Authorize(_ context.Context, request auth.Request) (auth.Decision, error) {
	p.requests = append(p.requests, request)
	return auth.Decision{Allowed: request.Action == ActionCreate, Spec: spec.All(), Assign: []auth.Assignment{{Dimension: "owner", Value: "alice"}}}, nil
}

func TestCreationValidatorMustPreserveAuthoritativeAssignments(t *testing.T) {
	for _, path := range []string{"create", "new", "new-bulk"} {
		for _, violates := range []bool{true, false} {
			t.Run(path+"/"+map[bool]string{true: "reject-mutation", false: "keep-assignment"}[violates], func(t *testing.T) {
				repo, err := testkit.NewAuditedMemoryRepository[*creationAssignedEntity, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*creationAssignedEntity, int64](app.ResourceContract{Assignable: []meta.Dimension{"owner"}}))
				if err != nil {
					t.Fatal(err)
				}
				policy := &creationAssignmentPolicy{}
				application, err := NewApplication(repo, Options[*creationAssignedEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeletePurge, Audit: repo, Validator: restoreValidationFunc(func(value any) error {
					entity := value.(*creationAssignedEntity)
					if entity.Owner != "alice" {
						t.Fatalf("validator did not see authoritative assignment: %q", entity.Owner)
					}
					if violates && entity.Name == "second" {
						entity.Owner = "mallory"
					}
					entity.Name += " validated"
					return nil
				})})
				if err != nil {
					t.Fatal(err)
				}
				ctx := newOperatorContext(t)
				first, second := &creationAssignedEntity{Name: "first"}, &creationAssignedEntity{Name: "second"}
				switch path {
				case "create":
					err = application.Create(ctx, second)
				case "new":
					_, err = application.ExecuteNew(ctx, ActionCreate, func(context.Context) (*creationAssignedEntity, error) { return second, nil })
				case "new-bulk":
					_, err = application.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*creationAssignedEntity, error) {
						return []*creationAssignedEntity{first, second}, nil
					})
				}
				if violates {
					if !errors.Is(err, errors.Forbidden) {
						t.Fatalf("validator replaced authoritative assignment: %v", err)
					}
					if second.Owner != "mallory" {
						t.Fatal("assignment was silently reapplied instead of rejecting validator output")
					}
				} else if err != nil {
					t.Fatalf("valid assignment rejected: %v", err)
				}
				if len(policy.requests) != 1 || policy.requests[0].Action != ActionCreate {
					t.Fatalf("creation authorization changed: %+v", policy.requests)
				}
				want := int64(1)
				if path == "new-bulk" {
					want = 2
				}
				if violates {
					want = 0
				}
				count, err := repo.Count(ctx, spec.All())
				if err != nil || count != want {
					t.Fatalf("wrong committed rows: count=%d want=%d err=%v", count, want, err)
				}
				for _, id := range []int64{1, 2} {
					records, err := repo.ListAuditRecords(ctx, strconv.FormatInt(id, 10), 0, 10, spec.All())
					if err != nil {
						t.Fatal(err)
					}
					if violates && len(records) != 0 {
						t.Fatalf("rejected/rolled-back assignment wrote audit for %d: %+v", id, records)
					}
				}
				if !violates {
					stored, err := repo.Get(ctx, second.ID, spec.All())
					if err != nil || stored.Owner != "alice" || stored.Name != "second validated" {
						t.Fatalf("valid authority did not persist: %+v %v", stored, err)
					}
					records, err := repo.ListAuditRecords(ctx, strconv.FormatInt(second.ID, 10), 0, 10, spec.All())
					if err != nil || len(records) != 1 || records[0].Action != ActionCreate {
						t.Fatalf("valid creation audit changed: %+v %v", records, err)
					}
				}
			})
		}
	}
}
