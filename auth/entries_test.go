package auth_test

import (
	"gochen/auth"
	"gochen/errors"
	"testing"
)

func TestEntryValidationRequiresIndependentValidExecutionBindings(t *testing.T) {
	d, options := declarationFixture(t)
	a, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := auth.BindExecution(a, d.Descriptor, auth.ResourceAccessData, "document assembly", "Application.Count -> scoped repository", "read")
	if err != nil {
		t.Fatal(err)
	}
	entry := auth.Entry{Kind: "command", Key: "document.read"}
	binding.Entry = entry
	public := auth.EntryBinding{Entry: auth.Entry{Kind: "http", Key: "GET /health"}, ResourceAccess: auth.ResourceAccessNone, Source: "health handler", AllowReason: "liveness"}
	report, err := auth.ValidateEntries([]auth.Entry{entry, public.Entry}, []auth.EntryBinding{binding, public})
	if err != nil || len(report) != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	classes := map[auth.EntryClass]int{}
	for _, item := range report {
		classes[item.Class]++
	}
	if classes[auth.EntryDeclared] != 1 || classes[auth.EntryAllowed] != 1 || classes[auth.EntryInvalid] != 0 {
		t.Fatalf("classes=%v", classes)
	}
	for _, test := range []struct {
		name     string
		actual   []auth.Entry
		bindings []auth.EntryBinding
	}{
		{"missing", []auth.Entry{entry}, nil},
		{"stale", nil, []auth.EntryBinding{binding}},
		{"duplicate actual", []auth.Entry{entry, entry}, []auth.EntryBinding{binding}},
		{"duplicate binding", []auth.Entry{entry}, []auth.EntryBinding{binding, binding}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := auth.ValidateEntries(test.actual, test.bindings)
			if !errors.Is(err, errors.InvalidInput) || len(report) != 1 || report[0].Class != auth.EntryInvalid || report[0].Diagnostic == "" {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		change func(*auth.EntryBinding)
	}{
		{"missing access", func(b *auth.EntryBinding) { b.ResourceAccess = auth.ResourceAccessUnspecified }},
		{"access conflict", func(b *auth.EntryBinding) { b.ResourceAccess = auth.ResourceAccessNone }},
		{"missing source", func(b *auth.EntryBinding) { b.Source = "" }},
		{"missing condition execution", func(b *auth.EntryBinding) { b.ConditionPath = "" }},
		{"unknown action", func(b *auth.EntryBinding) { b.Action = "missing" }},
		{"modified compiled plan", func(b *auth.EntryBinding) { plan := b.Plan.Copy(); plan.Descriptor.Resource = "other"; b.Plan = &plan }},
		{"resource allow without plan", func(b *auth.EntryBinding) { b.Plan = nil; b.AllowReason = "approved" }},
		{"forged plan", func(b *auth.EntryBinding) {
			b.Plan = &auth.Plan{Descriptor: d.Descriptor, ResourceAccess: auth.ResourceAccessData}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := binding
			test.change(&changed)
			report, err := auth.ValidateEntries([]auth.Entry{entry}, []auth.EntryBinding{changed})
			if !errors.Is(err, errors.InvalidInput) || report[0].Class != auth.EntryInvalid {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestMultiResourceEntryValidatesEveryExecutionPath(t *testing.T) {
	d, options := declarationFixture(t)
	a, err := auth.Compile(d, options)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := auth.BindExecution(a, d.Descriptor, auth.ResourceAccessData, "order command", "order conditions", "read")
	if err != nil {
		t.Fatal(err)
	}
	binding.Entry = auth.Entry{Kind: "command", Key: "order.cancel"}
	binding.Resources = []auth.ResourceBinding{{Name: "inventory", Action: "read", Plan: binding.Plan, Source: "stock command", ConditionPath: "product conditions"}}
	if report, err := auth.ValidateEntries([]auth.Entry{binding.Entry}, []auth.EntryBinding{binding}); err != nil || len(report) != 1 || report[0].Class != auth.EntryDeclared {
		t.Fatalf("multi resource report=%+v err=%v", report, err)
	}
	for _, change := range []func(*auth.ResourceBinding){
		func(r *auth.ResourceBinding) { r.Plan = nil },
		func(r *auth.ResourceBinding) { r.ConditionPath = "" },
		func(r *auth.ResourceBinding) { r.Action = "missing" },
	} {
		invalid := binding.Copy()
		change(&invalid.Resources[0])
		if _, err := auth.ValidateEntries([]auth.Entry{binding.Entry}, []auth.EntryBinding{invalid}); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("invalid additional resource=%v", err)
		}
	}
	frozen := binding.Copy()
	binding.Resources[0].Plan.Descriptor.Resource = "changed"
	if frozen.Resources[0].Plan.Descriptor.Resource == "changed" {
		t.Fatal("resource plan was shared")
	}
}
