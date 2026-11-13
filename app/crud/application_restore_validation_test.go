package crud

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type restoreGuardEntity struct {
	domcrud.TrackedEntity[int64]
	Tenant string  `dim:"tenant"`
	Owner  *string `dim:"owner"`
	Name   string
}
type restoreValidationFunc func(any) error

func (f restoreValidationFunc) Validate(v any) error { return f(v) }

type restoreTargetPolicy struct{ requests []auth.Request }

func (p *restoreTargetPolicy) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }
func (p *restoreTargetPolicy) Authorize(_ context.Context, request auth.Request) (auth.Decision, error) {
	p.requests = append(p.requests, request)
	return auth.Decision{Allowed: request.Action == ActionRestore && len(request.IDs) == 1 && request.IDs[0] == "1", Spec: spec.All()}, nil
}

func TestRestoreValidationCannotRedirectTargetOrChangeRestoredState(t *testing.T) {
	for _, mode := range []DeleteMode{DeleteSoft, DeletePurge} {
		for _, attack := range []string{"id", "version", "tenant", "owner", "owner-pointer", "soft-delete", "deleted-by"} {
			t.Run(map[DeleteMode]string{DeleteSoft: "soft", DeletePurge: "purge"}[mode]+"/"+attack, func(t *testing.T) {
				repo, err := testkit.NewAuditedMemoryRepository[*restoreGuardEntity, int64](nil)
				if err != nil {
					t.Fatal(err)
				}
				originals := []*restoreGuardEntity{}
				for _, id := range []int64{1, 2} {
					owner := "owner-a"
					entity := &restoreGuardEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: id}}, Tenant: "tenant-a", Owner: &owner, Name: "original"}
					if err := repo.Create(t.Context(), entity, spec.All()); err != nil {
						t.Fatal(err)
					}
					if err := entity.SoftDelete(time.Now()); err != nil {
						t.Fatal(err)
					}
					if err := repo.Update(t.Context(), entity, spec.All()); err != nil {
						t.Fatal(err)
					}
					stored, err := repo.GetWithDeleted(t.Context(), id, spec.All())
					if err != nil {
						t.Fatal(err)
					}
					originals = append(originals, stored)
				}
				policy := &restoreTargetPolicy{}
				after := false
				app, err := NewApplication(repo, Options[*restoreGuardEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: mode, EnableRestore: true, Audit: repo, Validator: restoreValidationFunc(func(value any) error {
					entity := value.(*restoreGuardEntity)
					entity.Name = "validator mutation"
					switch attack {
					case "id":
						entity.ID = 2
					case "version":
						entity.Version++
					case "tenant":
						entity.Tenant = "tenant-b"
					case "owner":
						other := "owner-b"
						entity.Owner = &other
					case "owner-pointer":
						*entity.Owner = "owner-b"
					case "soft-delete":
						return entity.SoftDelete(time.Now())
					case "deleted-by":
						by := "intruder"
						entity.DeletedBy = &by
					}
					return nil
				}), Hooks: Hooks[*restoreGuardEntity]{ActionRestore: {After: func(context.Context, *restoreGuardEntity) error { after = true; return nil }}}})
				if err != nil {
					t.Fatal(err)
				}
				result, err := app.Restore(newOperatorContext(t), 1)
				want := errors.Forbidden
				if attack == "soft-delete" || attack == "deleted-by" {
					want = errors.Conflict
				}
				if !errors.Is(err, want) || result != nil || after {
					t.Fatalf("validator mutation reached write: result=%+v err=%v after=%t", result, err, after)
				}
				if len(policy.requests) != 1 || policy.requests[0].Action != ActionRestore || !reflect.DeepEqual(policy.requests[0].IDs, []string{"1"}) {
					t.Fatalf("unexpected authorization: %+v", policy.requests)
				}
				for _, original := range originals {
					stored, err := repo.GetWithDeleted(t.Context(), original.ID, spec.All())
					if err != nil || !reflect.DeepEqual(stored, original) {
						t.Fatalf("rejected validator changed row: %+v want %+v err=%v", stored, original, err)
					}
					records, err := repo.ListAuditRecords(t.Context(), strconv.FormatInt(original.ID, 10), 0, 10, spec.All())
					if err != nil || len(records) != 0 {
						t.Fatalf("rejected validator wrote audit: %+v %v", records, err)
					}
				}
			})
		}
	}
}
