// 演示纯 Core 的强类型声明、资源范围执行和独立命令入口核对。
package main

import (
	"context"
	"log"

	"gochen/app/crud"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/messaging/command"
	"gochen/testkit"
)

type document struct {
	domcrud.Entity[int64]
	Department int64 `dim:"department"`
	Name       string
}

type departmentScope struct{}

func (departmentScope) ResolveSpec(context.Context, auth.Request) (spec.Spec, []auth.Assignment, error) {
	return spec.Eq(meta.ByDimension("department"), int64(10)), nil, nil
}

func (departmentScope) Vocabulary() auth.Vocabulary {
	return auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}}
}

func main() {
	log.SetFlags(0)
	repo, err := testkit.NewMemoryRepository[*document, int64](testkit.NewInt64Sequence(1))
	must(err)
	options := crud.Options[*document, int64]{DeleteMode: crud.DeletePurge, Resource: "document", ActionPermissions: map[string]string{
		"read": "document:api:read", "list": "document:api:read", "create": "document:api:write", "update": "document:api:write", "purge": "document:api:write",
	}}
	descriptor, err := crud.Describe(options, true)
	must(err)
	// 权限来自组合根装配的可信资料提供方；Core 保持身份与资料接口独立。
	checker := auth.ActionCheckerFunc(func(ctx context.Context, permission string) error {
		principal, err := auth.RequirePrincipal(ctx)
		if err != nil {
			return err
		}
		if principal.SubjectID == "employee" && permission == "document:api:read" {
			return nil
		}
		return errors.NewCode(errors.Forbidden, "permission denied")
	})
	options.Authorizer, err = auth.Compile(auth.Declaration{
		Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData,
		Rules: []auth.Rule{{ID: "document.department", Scope: &auth.ScopeRule{Resolver: departmentScope{}}}},
	}, auth.CompileOptions{Checker: checker, Metadata: repo.ExecutionDescriptor().Metadata()})
	must(err)
	application, err := crud.NewApplication(repo, options)
	must(err)
	seed, err := auth.WithPrincipal(context.Background(), auth.Principal{System: true})
	must(err)
	for _, item := range []*document{{Department: 10, Name: "visible"}, {Department: 20, Name: "outside"}} {
		must(application.Create(seed, item))
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "employee"})
	must(err)
	items, err := application.List(ctx, 0, 20)
	must(err)
	log.Printf("visible documents=%d name=%s", len(items), items[0].Name)
	_, err = application.Get(ctx, 2)
	log.Printf("outside document hidden=%t", errors.Is(err, errors.NotFound))
	called := false
	_, err = application.Execute(ctx, "update", 1, func(context.Context, *document) error { called = true; return nil })
	log.Printf("write denied=%t handler called=%t", errors.Is(err, errors.Forbidden), called)

	executor := command.NewCommandExecutor()
	must(executor.RegisterHandler("document.list", func(ctx context.Context, _ *command.Command) error {
		_, err := application.List(ctx, 0, 20)
		return err
	}))
	must(executor.SealHandlers())
	var actual []auth.Entry
	for _, name := range executor.RegisteredHandlers() {
		actual = append(actual, auth.Entry{Kind: "command", Key: name})
	}
	binding, err := application.AuthorizationBinding("list")
	must(err)
	binding.Entry = auth.Entry{Kind: "command", Key: "document.list"}
	report, err := auth.ValidateEntries(actual, []auth.EntryBinding{binding})
	must(err)
	log.Printf("registered entries=%d class=%s", len(report), report[0].Class)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
