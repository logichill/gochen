package spec

import (
	"gochen/domain/meta"
	"reflect"
	"strings"
	"testing"
)

type specEntity struct {
	ID       int64   `entity:"id"`
	TenantID string  `dim:"tenant"`
	ScopeID  int64   `dim:"scope"`
	OwnerID  *string `dim:"owner"`
	Name     string
	Age      int64
	Flag     bool
}

func strptr(s string) *string { return &s }

func TestSpecZeroAndIdentities(t *testing.T) {
	var zero Spec
	if !zero.IsNone() {
		t.Fatal("zero Spec must match nothing")
	}
	if !All().IsAll() {
		t.Fatal("All() must match everything")
	}
	if !And().IsAll() {
		t.Fatal("And() must equal All()")
	}
	if !Or().IsNone() {
		t.Fatal("Or() must equal zero")
	}
	if got := And(All(), Eq(meta.ByField("Name"), "a")).Canonical(); got != `eq(field:Name,"a")` {
		t.Fatalf("And(All, s) = %s", got)
	}
	if got := Or(zero, Eq(meta.ByField("Name"), "a")).Canonical(); got != `eq(field:Name,"a")` {
		t.Fatalf("Or(zero, s) = %s", got)
	}
	if !And(zero, Eq(meta.ByField("Name"), "a")).IsNone() {
		t.Fatal("And(zero, s) must be zero")
	}
	if !Or(All(), Eq(meta.ByField("Name"), "a")).IsAll() {
		t.Fatal("Or(All, s) must be All")
	}
}

func TestSpecCanonical(t *testing.T) {
	if got := In(meta.ByField("Name"), "b", "a", "b").Canonical(); got != `in(field:Name,"b","a")` {
		t.Fatalf("In canonical = %s", got)
	}
	if got := In(meta.ByField("Name"), "a").Canonical(); got != `eq(field:Name,"a")` {
		t.Fatalf("single In should rewrite to Eq, got %s", got)
	}
	if got := IsNull(meta.ByDimension("owner")).Canonical(); got != `isnull(dim:owner)` {
		t.Fatalf("IsNull canonical = %s", got)
	}
	if got := And(Eq(meta.ByField("b"), 2), And(Eq(meta.ByField("a"), 1), All())).Canonical(); got != "and(eq(field:a,1),eq(field:b,2))" {
		t.Fatalf("And canonical = %s", got)
	}
	if got := Or(Eq(meta.ByField("b"), 2), Or(Eq(meta.ByField("a"), 1), All())).Canonical(); got != "all" {
		t.Fatalf("Or with All should be All, got %s", got)
	}
	if got := Or(Eq(meta.ByField("b"), 2), Eq(meta.ByField("a"), 1)).Canonical(); got != "or(eq(field:a,1),eq(field:b,2))" {
		t.Fatalf("Or canonical = %s", got)
	}
	if got := Named("active_only", "arg1", 42).Canonical(); got != `named(active_only,"arg1",42)` {
		t.Fatalf("Named canonical = %s", got)
	}
}

func TestSpecInspection(t *testing.T) {
	s := And(
		Eq(meta.ByDimension("scope"), 10),
		Or(
			Eq(meta.ByDimension("owner"), "alice"),
			In(meta.ByField("Name"), "bob", "carol"),
		),
		Named("custom_guard", 1),
	)
	if !s.References(meta.ByDimension("scope")) || !s.References(meta.ByDimension("owner")) {
		t.Fatal("references role missing")
	}
	if s.References(meta.ByDimension("tenant")) {
		t.Fatal("tenant role should not be referenced")
	}
	if !s.HasNamed() {
		t.Fatal("expected named condition")
	}
	names := s.NamedNames()
	if len(names) != 1 || names[0] != "custom_guard" {
		t.Fatalf("unexpected named names: %v", names)
	}
	fields := s.Refs()
	want := []meta.Ref{meta.ByDimension("owner"), meta.ByDimension("scope"), meta.ByField("Name")}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
}

func TestSpecValidation(t *testing.T) {
	if s := Eq(meta.ByField(""), 1); s.Err() == nil {
		t.Fatal("empty field should fail")
	}
	if s := Eq(meta.ByDimension("tenant"), "t1"); s.Err() != nil {
		t.Fatal("dimension semantics belong to the execution contract")
	}
	if s := Eq(meta.ByField("@unknown"), "x"); s.Err() == nil {
		t.Fatal("unknown role should fail")
	}
	if s := Eq(meta.ByField("Name"), nil); s.Err() == nil {
		t.Fatal("nil value in Eq must fail")
	}
	if s := In(meta.ByField("Name"), "a", nil); s.Err() == nil {
		t.Fatal("nil in In must fail")
	}
	if s := Named(""); s.Err() == nil {
		t.Fatal("empty named condition must fail")
	}
	if s := And(Eq(meta.ByField("Name"), "a"), Eq(meta.ByDimension(" Tenant"), "t1")); s.Err() == nil {
		t.Fatal("composite with invalid child must fail")
	}
}

func TestInterpreterMatch(t *testing.T) {
	e := &specEntity{
		ID:       1,
		TenantID: "tenant-a",
		ScopeID:  42,
		OwnerID:  strptr("user-99"),
		Name:     "alice",
		Age:      30,
		Flag:     true,
	}

	i := NewInterpreter()

	cases := []struct {
		name string
		spec Spec
		want bool
	}{
		{"all", All(), true},
		{"none", Spec{}, false},
		{"eq name hit", Eq(meta.ByField("Name"), "alice"), true},
		{"eq name miss", Eq(meta.ByField("Name"), "bob"), false},
		{"role scope hit", Eq(meta.ByDimension("scope"), 42), true},
		{"role scope type coercion", Eq(meta.ByDimension("scope"), "42"), true},
		{"role owner hit", Eq(meta.ByDimension("owner"), "user-99"), true},
		{"in name hit", In(meta.ByField("Name"), "carol", "alice"), true},
		{"in name miss", In(meta.ByField("Name"), "carol", "dave"), false},
		{"is null false on ptr", IsNull(meta.ByDimension("owner")), false},
		{"and hit", And(Eq(meta.ByField("Name"), "alice"), Eq(meta.ByDimension("scope"), 42)), true},
		{"and miss", And(Eq(meta.ByField("Name"), "alice"), Eq(meta.ByDimension("scope"), 1)), false},
		{"or hit", Or(Eq(meta.ByField("Name"), "nobody"), Eq(meta.ByDimension("scope"), 42)), true},
		{"or miss", Or(Eq(meta.ByField("Name"), "nobody"), Eq(meta.ByDimension("scope"), 1)), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := i.Match(tc.spec, e)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("Match = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestInterpreterNullAndNamed(t *testing.T) {
	e := &specEntity{ID: 1, Name: "bob", OwnerID: nil}
	i := NewInterpreter(WithNamed("name_starts_with", func(entity any, args []any) (bool, error) {
		ent, ok := entity.(*specEntity)
		if !ok || len(args) == 0 {
			return false, nil
		}
		prefix, ok := args[0].(string)
		if !ok {
			return false, nil
		}
		return strings.HasPrefix(ent.Name, prefix), nil
	}))

	ok, err := i.Match(IsNull(meta.ByDimension("owner")), e)
	if err != nil || !ok {
		t.Fatalf("IsNull(RoleOwner) = %v, %v", ok, err)
	}

	ok, err = i.Match(Named("name_starts_with", "bo"), e)
	if err != nil || !ok {
		t.Fatalf("Named match = %v, %v", ok, err)
	}

	ok, err = i.Match(Named("name_starts_with", "al"), e)
	if err != nil || ok {
		t.Fatalf("Named mismatch = %v, %v", ok, err)
	}

	// 未注册命名条件报错
	_, err = i.Match(Named("missing_one"), e)
	if err == nil {
		t.Fatal("expected error on unregistered named condition")
	}
}

func TestViewKindAndFields(t *testing.T) {
	s := In(meta.ByField("Name"), "a", "b")
	if s.Kind() != KindIn {
		t.Fatalf("expected KindIn, got %v", s.Kind())
	}
	if s.Ref() != meta.ByField("Name") {
		t.Fatalf("expected Name, got %s", s.Ref())
	}
	if len(s.Values()) != 2 {
		t.Fatalf("expected 2 values, got %d", len(s.Values()))
	}
}
