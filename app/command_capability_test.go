package app_test

import (
	"context"
	"fmt"
	"testing"

	"gochen/app"
	"gochen/auth"
	"gochen/contextx"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestCommandReaderAppliesScopeAndRevokesRetainedCapability(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, department := range map[int64]int64{1: 10, 2: 20} {
		if err := repo.Create(t.Context(), &commandEntity{Entity: domcrud.Entity[int64]{ID: id}, Department: department}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	otherRepository, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: "count", Kind: auth.KindList, Permission: "document:api:read"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	var retained *app.CommandReader[struct{}, int64]
	command, err := app.NewCommand(app.CommandOptions[struct{}, *app.CommandReader[struct{}, int64], int64]{
		Resources: []app.CommandResource[struct{}]{{Name: "document", Repository: repo, Authorizer: authorizer, Action: "count", Intent: func(struct{}) (auth.Request, error) { return auth.Request{}, nil }, ConditionPath: "CommandReader -> scoped count"}},
		Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (*app.CommandReader[struct{}, int64], error) {
			if _, err := app.NewCommandReader(otherRepository, grants[0], func(ctx context.Context, _ struct{}, condition spec.Spec) (int64, error) {
				return otherRepository.Count(ctx, condition)
			}); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("reader bound to undeclared repository=%v", err)
			}
			return app.NewCommandReader(repo, grants[0], func(ctx context.Context, _ struct{}, condition spec.Spec) (int64, error) {
				return repo.Count(ctx, condition)
			})
		},
		Handler: func(ctx context.Context, _ struct{}, reader *app.CommandReader[struct{}, int64]) (int64, error) {
			retained = reader
			return reader.Read(ctx, struct{}{})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	count, err := command.Execute(t.Context(), struct{}{})
	if err != nil || count != 1 {
		t.Fatalf("scoped count=%d err=%v", count, err)
	}
	if _, err := retained.Read(t.Context(), struct{}{}); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained reader=%v", err)
	}
	binding, err := command.AuthorizationBinding()
	if err != nil || binding.ConditionPath != "CommandReader -> scoped count" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
}

func TestCommandReaderCombinesIntentIDsWithScope(t *testing.T) {
	repository, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, department := range map[int64]int64{1: 10, 2: 20} {
		if err := repository.Create(t.Context(), &commandEntity{Entity: domcrud.Entity[int64]{ID: id}, Department: department}, spec.All()); err != nil {
			t.Fatal(err)
		}
	}
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: "count", Kind: auth.KindList, Permission: "document:api:read"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Scope: &auth.ScopeRule{Resolver: commandScope{}}}}}, auth.CompileOptions{Metadata: repository.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandReader[struct{}, int64], int64]{
		Resources: []app.CommandResource[int64]{{Name: "document", Repository: repository, Authorizer: authorizer, Action: "count", Intent: func(id int64) (auth.Request, error) { return auth.Request{IDs: []string{fmt.Sprint(id)}}, nil }}},
		Access: func(_ context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandReader[struct{}, int64], error) {
			return app.NewCommandReader(repository, grants[0], func(ctx context.Context, _ struct{}, condition spec.Spec) (int64, error) {
				return repository.Count(ctx, condition)
			})
		},
		Handler: func(ctx context.Context, _ int64, reader *app.CommandReader[struct{}, int64]) (int64, error) {
			return reader.Read(ctx, struct{}{})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]int64{1: 1, 2: 0} {
		if count, err := command.Execute(t.Context(), id); err != nil || count != want {
			t.Fatalf("intent ID=%d count=%d err=%v", id, count, err)
		}
	}
}

type identityConstraintPolicy struct {
	condition spec.Spec
	assign    []auth.Assignment
}

func TestCommandRevokesRetainedFactsBeforeDeferredResourceReads(t *testing.T) {
	repository, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	loads := 0
	fact, err := auth.NewFact("document.owner", func(context.Context, auth.Request) (string, error) { loads++; return "alice", nil })
	if err != nil {
		t.Fatal(err)
	}
	policy, err := auth.WithFacts(auth.PolicyFunc(func(context.Context, auth.Request) (auth.Decision, error) {
		return auth.Decision{Allowed: true, Spec: spec.All()}, nil
	}), fact)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "document", Actions: []auth.Action{{Name: "read", Kind: auth.KindRead, Permission: "document:api:read"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Constraints: []auth.IPolicy{policy}}}}, auth.CompileOptions{Metadata: repository.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	var retainedGrant *app.CommandGrant
	command, err := app.NewCommand(app.CommandOptions[struct{}, auth.Request, auth.Request]{
		Resources: []app.CommandResource[struct{}]{{Name: "document", Repository: repository, Authorizer: authorizer, Action: "read", Intent: func(struct{}) (auth.Request, error) { return auth.Request{}, nil }}},
		Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (auth.Request, error) {
			retainedGrant = grants[0]
			return grants[0].Request()
		},
		Handler: func(_ context.Context, _ struct{}, request auth.Request) (auth.Request, error) { return request, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := command.Execute(t.Context(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fact.Get(t.Context(), request); !errors.Is(err, errors.FailedPrecondition) || loads != 0 {
		t.Fatalf("deferred fact read=%v loads=%d", err, loads)
	}
	if _, err := retainedGrant.Request(); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained grant request=%v", err)
	}
}

func (p identityConstraintPolicy) Evaluate(context.Context, auth.Request) (auth.Decision, error) {
	return auth.Decision{Allowed: true, Spec: p.condition, Assign: p.assign}, nil
}
func (p identityConstraintPolicy) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ID(), meta.ByDimension("department")}, Assignable: []meta.Dimension{"department"}}
}

func TestCommandIdentityCallRejectsFieldConditionsAndAssignmentsBeforeHandler(t *testing.T) {
	for _, test := range []struct {
		name      string
		condition spec.Spec
		assign    []auth.Assignment
	}{
		{"field condition", spec.Eq(meta.ByDimension("department"), int64(10)), nil},
		{"assignment", spec.Eq(meta.ID(), int64(1)), []auth.Assignment{{Dimension: "department", Value: int64(10)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, err := testkit.NewMemoryRepository[*commandEntity, int64](nil, testkit.WithMemoryResourceContract[*commandEntity, int64](app.ResourceContract{Assignable: []meta.Dimension{"department"}}))
			if err != nil {
				t.Fatal(err)
			}
			authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "points", Actions: []auth.Action{{Name: "deduct", Kind: auth.KindUpdate, Permission: "order:api:write"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Constraints: []auth.IPolicy{identityConstraintPolicy{condition: test.condition, assign: test.assign}}}}}, auth.CompileOptions{Metadata: repository.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
			if err != nil {
				t.Fatal(err)
			}
			calls, handlers := 0, 0
			command, err := app.NewCommand(app.CommandOptions[struct{}, *app.CommandIdentityCall[int64, int64, int64], int64]{
				Resources: []app.CommandResource[struct{}]{{Name: "points", Repository: repository, Authorizer: authorizer, Action: "deduct", Intent: func(struct{}) (auth.Request, error) { return auth.Request{}, nil }}},
				Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (*app.CommandIdentityCall[int64, int64, int64], error) {
					return app.NewCommandIdentityCall(repository, grants[0], func(id int64) int64 { return id }, func(_ context.Context, id int64) (int64, error) { calls++; return id, nil })
				},
				Handler: func(ctx context.Context, _ struct{}, access *app.CommandIdentityCall[int64, int64, int64]) (int64, error) {
					handlers++
					return access.Call(ctx, 1)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := command.Execute(t.Context(), struct{}{}); !errors.Is(err, errors.InvalidInput) || calls != 0 || handlers != 0 {
				t.Fatalf("unsupported service constraints err=%v calls=%d handlers=%d", err, calls, handlers)
			}
		})
	}
}

type identityCallPolicy struct{}

func (identityCallPolicy) Evaluate(context.Context, auth.Request) (auth.Decision, error) {
	return auth.Decision{Allowed: true, Spec: spec.Eq(meta.ID(), int64(1))}, nil
}
func (identityCallPolicy) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ID()}}
}

type identityCallInput struct{ ID int64 }

func TestCommandIdentityCallRejectsOtherTargetsAndRevokesCapability(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "points", Actions: []auth.Action{{Name: "deduct", Kind: auth.KindUpdate, Permission: "order:api:write"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{Constraints: []auth.IPolicy{identityCallPolicy{}}}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var retained *app.CommandIdentityCall[*identityCallInput, int64, int64]
	command, err := app.NewCommand(app.CommandOptions[struct{}, *app.CommandIdentityCall[*identityCallInput, int64, int64], int64]{
		Resources: []app.CommandResource[struct{}]{{Name: "points", Repository: repo, Authorizer: authorizer, Action: "deduct", Intent: func(struct{}) (auth.Request, error) { return auth.Request{}, nil }}},
		Access: func(_ context.Context, _ struct{}, grants []*app.CommandGrant) (*app.CommandIdentityCall[*identityCallInput, int64, int64], error) {
			return app.NewCommandIdentityCall(repo, grants[0], func(input *identityCallInput) int64 {
				id := input.ID
				input.ID = 2
				return id
			}, func(_ context.Context, input *identityCallInput) (int64, error) { calls++; return input.ID, nil })
		},
		Handler: func(ctx context.Context, _ struct{}, caller *app.CommandIdentityCall[*identityCallInput, int64, int64]) (int64, error) {
			retained = caller
			if _, err := caller.Call(ctx, &identityCallInput{ID: 2}); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("other target=%v", err)
			}
			return caller.Call(ctx, &identityCallInput{ID: 1})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := command.Execute(t.Context(), struct{}{})
	if err != nil || result != 1 || calls != 1 {
		t.Fatalf("identity call=%d calls=%d err=%v", result, calls, err)
	}
	if _, err := retained.Call(t.Context(), &identityCallInput{ID: 1}); !errors.Is(err, errors.FailedPrecondition) {
		t.Fatalf("retained call=%v", err)
	}
}

type identityCallSummary struct {
	Completed []int64
	Callback  func()
}

func TestCommandIdentityCallPreservesIndependentPartialResultsAndErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		callErr    error
		uncopyable bool
		deny       bool
	}{
		{name: "success"},
		{name: "partial failure", callErr: errors.NewCode(errors.Internal, "later batch failed")},
		{name: "after commit", callErr: contextx.WrapAfterCommitError(errors.NewCode(errors.Internal, "publish failed"))},
		{name: "copy failure", uncopyable: true},
		{name: "combined failure", uncopyable: true, callErr: contextx.WrapAfterCommitError(errors.NewCode(errors.Internal, "publish failed"))},
		{name: "authorization failure", deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, err := testkit.NewMemoryRepository[*commandEntity, int64](nil)
			if err != nil {
				t.Fatal(err)
			}
			authorizer, err := auth.Compile(auth.Declaration{Descriptor: auth.Descriptor{Resource: "ledger", Actions: []auth.Action{{Name: "cleanup", Kind: auth.KindDelete, Permission: "ledger:api:write"}}}, ResourceAccess: auth.ResourceAccessData, Rules: []auth.Rule{{}}}, auth.CompileOptions{Metadata: repo.ExecutionDescriptor().Metadata(), Checker: auth.ActionCheckerFunc(func(context.Context, string) error {
				if test.deny {
					return errors.NewCode(errors.Forbidden, "denied")
				}
				return nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			original := &identityCallSummary{Completed: []int64{1, 2}}
			if test.uncopyable {
				original.Callback = func() {}
			}
			calls := 0
			var retained *app.CommandIdentityCall[int64, int64, *identityCallSummary]
			command, err := app.NewCommand(app.CommandOptions[int64, *app.CommandIdentityCall[int64, int64, *identityCallSummary], *identityCallSummary]{
				Resources: []app.CommandResource[int64]{{Name: "ledger", Repository: repo, Authorizer: authorizer, Action: "cleanup", Intent: func(id int64) (auth.Request, error) { return auth.Request{IDs: []string{fmt.Sprint(id)}}, nil }}},
				Access: func(_ context.Context, _ int64, grants []*app.CommandGrant) (*app.CommandIdentityCall[int64, int64, *identityCallSummary], error) {
					return app.NewCommandIdentityCall(repo, grants[0], func(id int64) int64 { return id }, func(context.Context, int64) (*identityCallSummary, error) { calls++; return original, test.callErr })
				},
				Handler: func(ctx context.Context, id int64, access *app.CommandIdentityCall[int64, int64, *identityCallSummary]) (*identityCallSummary, error) {
					retained = access
					return access.Call(ctx, id)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.Execute(t.Context(), 1)
			if test.deny {
				if output != nil || calls != 0 || !errors.Is(err, errors.Forbidden) {
					t.Fatalf("denied output=%+v calls=%d err=%v", output, calls, err)
				}
				return
			}
			if test.callErr != nil && !errors.Is(err, test.callErr) {
				t.Fatalf("lost callback error: %v", err)
			}
			if contextx.IsAfterCommitError(err) != contextx.IsAfterCommitError(test.callErr) {
				t.Fatalf("lost after-commit marker: %v", err)
			}
			if test.uncopyable {
				if output != nil || !errors.Is(err, errors.Unsupported) {
					t.Fatalf("unsafe output=%+v err=%v", output, err)
				}
			} else {
				if output == nil || output == original || len(output.Completed) != 2 {
					t.Fatalf("partial output=%+v err=%v", output, err)
				}
				output.Completed[0] = 99
				if original.Completed[0] != 1 {
					t.Fatal("output aliases service-owned result")
				}
				if test.callErr == nil && err != nil {
					t.Fatal(err)
				}
			}
			if _, err := retained.Call(t.Context(), 1); !errors.Is(err, errors.FailedPrecondition) || calls != 1 {
				t.Fatalf("failure retained capability err=%v calls=%d", err, calls)
			}
		})
	}
}
