package deletion

import (
	"context"
	appcrud "gochen/app/crud"
	audited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/testkit"
	"gochen/testkit/contracttest"
	"testing"
)

func TestOrdinaryWritesKeepAuditFactsIndependentOfPresentation(t *testing.T) {
	for _, action := range []string{"create", "update", "delete", "restore", "purge", "rename", "import", "rename_bulk", "import_bulk"} {
		t.Run(action, func(t *testing.T) {
			repo, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1),
				testkit.WithMemoryResourceContract[*contracttest.Document, int64](contracttest.Contract()),
				testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
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
			region := "north"
			entity := &contracttest.Document{Department: 1, Region: &region, Name: "before"}
			if action != "create" && action != "import" && action != "import_bulk" {
				if err := repo.Create(ctx, entity, spec.All()); err != nil {
					t.Fatal(err)
				}
			}
			if action == "restore" || action == "purge" {
				if err := entity.SoftDeleteBy("alice", entity.CreatedAt); err != nil {
					t.Fatal(err)
				}
				if err := repo.Update(ctx, entity, spec.All()); err != nil {
					t.Fatal(err)
				}
			}
			afterCalls, postCalls := 0, 0
			mutate := func(e *contracttest.Document) { e.Department = 999; *e.Region = "changed"; e.ID = 999 }
			application, err := newTestApplication(repo, repo, testOptions[*contracttest.Document, int64]{
				Options: appcrud.Options[*contracttest.Document, int64]{
					Resource: "document", Authorizer: &transferPolicy{},
					Actions: []auth.Action{{Name: "rename", Kind: auth.KindUpdate}, {Name: "rename_bulk", Kind: auth.KindUpdate}, {Name: "import", Kind: auth.KindCreate}, {Name: "import_bulk", Kind: auth.KindCreate}},
					Hooks: appcrud.Hooks[*contracttest.Document]{action: {
						After: func(_ context.Context, e *contracttest.Document) error { afterCalls++; mutate(e); return nil },
						PostCommit: func(_ context.Context, e *contracttest.Document) error {
							postCalls++
							if e.Department != 1 || *e.Region != "north" || e.ID != 1 {
								t.Errorf("post-commit received polluted state: %+v", e)
							}
							mutate(e)
							return nil
						},
					}},
				},
				Projector: func(e *contracttest.Document) (any, error) {
					view := map[string]any{"name": e.Name, "department": e.Department, "region": *e.Region}
					mutate(e)
					return view, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			rename := func(_ context.Context, e *contracttest.Document) error { e.Name = "after"; return nil }
			switch action {
			case "create":
				err = application.Create(ctx, entity)
			case "update":
				entity.Name = "after"
				err = application.Update(ctx, entity)
			case "delete":
				err = application.Delete(ctx, entity.ID)
			case "restore":
				entity, err = application.Restore(ctx, entity.ID)
			case "purge":
				err = application.Purge(ctx, entity.ID)
			case "rename":
				entity, err = application.Execute(ctx, action, entity.ID, rename)
			case "import":
				entity, err = application.ExecuteNew(ctx, action, func(context.Context) (*contracttest.Document, error) { return entity, nil })
			case "rename_bulk":
				_, err = application.ExecuteBulk(ctx, action, []int64{entity.ID}, rename)
			case "import_bulk":
				_, err = application.ExecuteNewBulk(ctx, action, func(context.Context) ([]*contracttest.Document, error) { return []*contracttest.Document{entity}, nil })
			}
			if err != nil {
				t.Fatal(err)
			}
			if afterCalls != 1 || postCalls != 1 {
				t.Fatalf("hooks after=%d post=%d", afterCalls, postCalls)
			}
			if entity.ID != 1 || entity.Department != 1 || *entity.Region != "north" {
				t.Fatalf("caller state polluted: %+v", entity)
			}
			records, err := repo.ListAuditRecords(audited.WithAuditResourceKind(ctx, "document"), "1", 0, 10, spec.All())
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 {
				t.Fatalf("records=%+v", records)
			}
			department, _ := records[0].Dimensions.Value(meta.Dimension("department"))
			storedRegion, _ := records[0].Dimensions.Value(meta.Dimension("region"))
			if department != int64(1) || storedRegion != "north" {
				t.Fatalf("audit facts polluted: %+v", records[0])
			}
		})
	}
}
