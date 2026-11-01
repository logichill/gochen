package spec

import (
	"strings"
	"testing"
)

type specEntity struct {
	ID       int64   `role:"id"`
	TenantID string  `role:"tenant"`
	ScopeID  int64   `role:"scope"`
	OwnerID  *string `role:"owner"`
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
	if got := And(All(), Eq("Name", "a")).Canonical(); got != `eq(Name,"a")` {
		t.Fatalf("And(All, s) = %s", got)
	}
	if got := Or(zero, Eq("Name", "a")).Canonical(); got != `eq(Name,"a")` {
		t.Fatalf("Or(zero, s) = %s", got)
	}
	if !And(zero, Eq("Name", "a")).IsNone() {
		t.Fatal("And(zero, s) must be zero")
	}
	if !Or(All(), Eq("Name", "a")).IsAll() {
		t.Fatal("Or(All, s) must be All")
	}
}

func TestSpecCanonical(t *testing.T) {
	if got := In("Name", "b", "a", "b").Canonical(); got != `in(Name,"b","a")` {
		t.Fatalf("In canonical = %s", got)
	}
	if got := In("Name", "a").Canonical(); got != `eq(Name,"a")` {
		t.Fatalf("single In should rewrite to Eq, got %s", got)
	}
	if got := IsNull(RoleOwner).Canonical(); got != `isnull(@owner)` {
		t.Fatalf("IsNull canonical = %s", got)
	}
	if got := And(Eq("b", 2), And(Eq("a", 1), All())).Canonical(); got != "and(eq(a,1),eq(b,2))" {
		t.Fatalf("And canonical = %s", got)
	}
	if got := Or(Eq("b", 2), Or(Eq("a", 1), All())).Canonical(); got != "all" {
		t.Fatalf("Or with All should be All, got %s", got)
	}
	if got := Or(Eq("b", 2), Eq("a", 1)).Canonical(); got != "or(eq(a,1),eq(b,2))" {
		t.Fatalf("Or canonical = %s", got)
	}
	if got := Named("active_only", "arg1", 42).Canonical(); got != `named(active_only,"arg1",42)` {
		t.Fatalf("Named canonical = %s", got)
	}
}

func TestSpecInspection(t *testing.T) {
	s := And(
		Eq(RoleScope, 10),
		Or(
			Eq(RoleOwner, "alice"),
			In("Name", "bob", "carol"),
		),
		Named("custom_guard", 1),
	)
	if !s.ReferencesRole(RoleScope) || !s.ReferencesRole(RoleOwner) {
		t.Fatal("references role missing")
	}
	if s.ReferencesRole(RoleTenant) {
		t.Fatal("tenant role should not be referenced")
	}
	if !s.HasNamed() {
		t.Fatal("expected named condition")
	}
	names := s.NamedNames()
	if len(names) != 1 || names[0] != "custom_guard" {
		t.Fatalf("unexpected named names: %v", names)
	}
	fields := s.Fields()
	want := []string{"@owner", "@scope", "Name"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
}

func TestSpecValidation(t *testing.T) {
	if s := Eq("", 1); s.Err() == nil {
		t.Fatal("empty field should fail")
	}
	if s := Eq(RoleTenant, "t1"); s.Err() == nil {
		t.Fatal("tenant role in spec must be rejected")
	}
	if s := Eq("@unknown", "x"); s.Err() == nil {
		t.Fatal("unknown role should fail")
	}
	if s := Eq("Name", nil); s.Err() == nil {
		t.Fatal("nil value in Eq must fail")
	}
	if s := In("Name", "a", nil); s.Err() == nil {
		t.Fatal("nil in In must fail")
	}
	if s := Named(""); s.Err() == nil {
		t.Fatal("empty named condition must fail")
	}
	if s := And(Eq("Name", "a"), Eq(RoleTenant, "t1")); s.Err() == nil {
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
		{"eq name hit", Eq("Name", "alice"), true},
		{"eq name miss", Eq("Name", "bob"), false},
		{"role scope hit", Eq(RoleScope, 42), true},
		{"role scope type coercion", Eq(RoleScope, "42"), true},
		{"role owner hit", Eq(RoleOwner, "user-99"), true},
		{"in name hit", In("Name", "carol", "alice"), true},
		{"in name miss", In("Name", "carol", "dave"), false},
		{"is null false on ptr", IsNull(RoleOwner), false},
		{"and hit", And(Eq("Name", "alice"), Eq(RoleScope, 42)), true},
		{"and miss", And(Eq("Name", "alice"), Eq(RoleScope, 1)), false},
		{"or hit", Or(Eq("Name", "nobody"), Eq(RoleScope, 42)), true},
		{"or miss", Or(Eq("Name", "nobody"), Eq(RoleScope, 1)), false},
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

	ok, err := i.Match(IsNull(RoleOwner), e)
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
	s := In("Name", "a", "b")
	if s.Kind() != KindIn {
		t.Fatalf("expected KindIn, got %v", s.Kind())
	}
	if s.Field() != "Name" {
		t.Fatalf("expected Name, got %s", s.Field())
	}
	if len(s.Values()) != 2 {
		t.Fatalf("expected 2 values, got %d", len(s.Values()))
	}
}
