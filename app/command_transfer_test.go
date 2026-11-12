package app_test

import (
	"context"
	"reflect"
	"testing"

	"gochen/app"
	"gochen/app/audited"
	"gochen/app/crud"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/contracttest"
)

type commandTransferPolicy struct{ actions []string }

func (*commandTransferPolicy) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}, Assignable: []meta.Dimension{"department"}}
}
func (p *commandTransferPolicy) Evaluate(_ context.Context, req auth.Request) (auth.Decision, error) {
	p.actions = append(p.actions, req.Action)
	switch req.Action {
	case "move":
		return auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("department"), 1)}, nil
	case "receive":
		return auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("department"), 2), Assign: []auth.Assignment{{Dimension: "department", Value: 2}}}, nil
	default:
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	}
}

type commandTransferApplication interface {
	Transfer(context.Context, string, int64, uint64, []auth.Assignment, ...crud.ExecOption) (*contracttest.Document, error)
}

func TestCommandExecutionRejectsTransferDuringAndAfterCommand(t *testing.T) {
	for _, kind := range []string{"crud", "audited"} {
		t.Run(kind, func(t *testing.T) {
			ctx, err := contextx.WithTenantID(t.Context(), "tenant")
			if err != nil {
				t.Fatal(err)
			}
			ctx, err = auth.WithPrincipal(ctx, auth.Principal{SubjectID: "member"})
			if err != nil {
				t.Fatal(err)
			}
			repo, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1), testkit.WithMemoryResourceContract[*contracttest.Document, int64](contracttest.Contract()), testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
			if err != nil {
				t.Fatal(err)
			}
			item := &contracttest.Document{Name: "source", Department: 1}
			if err := repo.Create(ctx, item, spec.All()); err != nil {
				t.Fatal(err)
			}
			options := crud.Options[*contracttest.Document, int64]{Resource: "document", Actions: []auth.Action{{Name: "move", Kind: auth.KindTransfer, ReceiveAction: "receive", Dimensions: []meta.Dimension{"department"}}, {Name: "receive", Kind: auth.KindTransfer}}}
			descriptor, err := crud.Describe(options, true)
			if kind == "audited" {
				descriptor, err = audited.Describe(audited.Options[*contracttest.Document, int64]{Options: options})
			}
			if err != nil {
				t.Fatal(err)
			}
			policy := &commandTransferPolicy{}
			authorizer, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Constraints: []auth.IPolicy{policy}}}}, auth.CompileOptions{Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil }), Metadata: repo.ExecutionDescriptor().Metadata()})
			if err != nil {
				t.Fatal(err)
			}
			options.Authorizer = authorizer
			construct := func(grant *app.CommandGrant) (commandTransferApplication, error) {
				opts := options
				opts.CommandGrant = grant
				if kind == "audited" {
					return audited.NewApplication(repo, repo, audited.Options[*contracttest.Document, int64]{Options: opts})
				}
				return crud.NewApplication(repo, opts)
			}
			targets := []auth.Assignment{{Dimension: "department", Value: 2}}
			check := func(application commandTransferApplication) {
				t.Helper()
				for _, call := range []struct {
					action string
					id     int64
				}{{"move", item.ID}, {"move", item.ID + 1}, {"receive", item.ID}, {"undeclared", item.ID}} {
					if _, err := application.Transfer(ctx, call.action, call.id, item.Version, targets); !errors.Is(err, errors.FailedPrecondition) {
						t.Fatalf("Transfer(%s,%d)=%v", call.action, call.id, err)
					}
				}
			}
			var retained commandTransferApplication
			command, err := app.NewCommand(app.CommandOptions[struct{}, commandTransferApplication, struct{}]{Resources: []app.CommandResource[struct{}]{{Name: "document", Repository: repo, Authorizer: authorizer, Action: "update", Intent: func(struct{}) (auth.Request, error) { return auth.Request{IDs: []string{"1"}}, nil }}},
				Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (commandTransferApplication, error) {
					return construct(grants[0])
				},
				Handler: func(_ context.Context, _ struct{}, application commandTransferApplication) (struct{}, error) {
					retained = application
					check(application)
					return struct{}{}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := command.Execute(ctx, struct{}{}); err != nil {
				t.Fatal(err)
			}
			check(retained)
			if !reflect.DeepEqual(policy.actions, []string{"update"}) {
				t.Fatalf("unsupported transfer reached authorization: %v", policy.actions)
			}
			stored, err := repo.Get(ctx, item.ID, spec.All())
			if err != nil || stored.Department != 1 || stored.Version != item.Version {
				t.Fatalf("rejected transfer changed source: %+v %v", stored, err)
			}
			plain, err := construct(nil)
			if err != nil {
				t.Fatal(err)
			}
			after, err := plain.Transfer(ctx, "move", item.ID, item.Version, targets)
			if err != nil || after.Department != 2 || after.Version != item.Version+1 {
				t.Fatalf("ordinary transfer: %+v %v", after, err)
			}
			if !reflect.DeepEqual(policy.actions, []string{"update", "move", "receive"}) {
				t.Fatalf("ordinary two-phase authorization: %v", policy.actions)
			}
		})
	}
}
