package meta

import (
	"reflect"
	"testing"
)

type MetaBase struct {
	ID      int64  `entity:"id" managed:"true"`
	Version uint64 `managed:"true"`
}

type MetaEntity struct {
	MetaBase
	TenantID string  `dim:"tenant"`
	ScopeID  int64   `dim:"scope"`
	OwnerID  *string `dim:"owner"`
	Name     string
	Enabled  bool
	Small    int8
}

func TestDescribe(t *testing.T) {
	m, err := Describe[*MetaEntity]()
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got, want := m.Type().Name(), "MetaEntity"; got != want {
		t.Fatalf("Type().Name() = %q, want %q", got, want)
	}

	id, ok := m.IDField()
	if !ok {
		t.Fatal("RoleID field not found")
	}
	if id.Name != "ID" || id.Type.Kind() != reflect.Int64 {
		t.Fatalf("RoleID = %s/%v, want ID/int64", id.Name, id.Type)
	}
	if !id.Managed {
		t.Fatal("ID should be managed")
	}

	owner, ok := m.DimensionField("owner")
	if !ok {
		t.Fatal("RoleOwner field not found")
	}
	if owner.Name != "OwnerID" || owner.Type.Kind() != reflect.Ptr {
		t.Fatalf("RoleOwner = %s/%v, want OwnerID/*string", owner.Name, owner.Type)
	}

	if _, ok := m.Field("Version"); !ok {
		t.Fatal("Version field not found")
	}
	if got := len(m.ManagedFields()); got != 2 {
		t.Fatalf("ManagedFields len = %d, want 2", got)
	}
}

func TestDescribeRejectsNonPointer(t *testing.T) {
	if _, err := Describe[MetaEntity](); err == nil {
		t.Fatal("expected error for non-pointer entity type")
	}
}

func TestDescribeRejectsDuplicateDimension(t *testing.T) {
	type Dup struct {
		A int64 `dim:"scope"`
		B int64 `dim:"scope"`
	}
	if _, err := Describe[*Dup](); err == nil {
		t.Fatal("expected error for duplicate role")
	}
}

func TestDescribeRejectsInvalidDimension(t *testing.T) {
	type Bad struct {
		A int64 `dim:" Bad"`
	}
	if _, err := Describe[*Bad](); err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestFieldValueAndNull(t *testing.T) {
	owner := "alice"
	e := &MetaEntity{
		MetaBase: MetaBase{ID: 7},
		TenantID: "t1",
		ScopeID:  3,
		OwnerID:  &owner,
		Name:     "n",
		Enabled:  true,
	}
	m, err := DescribeValue(e)
	if err != nil {
		t.Fatalf("DescribeValue: %v", err)
	}

	ownerField, _ := m.DimensionField("owner")
	isNull, err := ownerField.IsNull(e)
	if err != nil || isNull {
		t.Fatalf("IsNull(owner) = %v, %v; want false, nil", isNull, err)
	}
	isZero, err := ownerField.IsZero(e)
	if err != nil || isZero {
		t.Fatalf("IsZero(owner) = %v, %v; want false, nil", isZero, err)
	}

	e.OwnerID = nil
	isNull, err = ownerField.IsNull(e)
	if err != nil || !isNull {
		t.Fatalf("IsNull(nil owner) = %v, %v; want true, nil", isNull, err)
	}
	isZero, err = ownerField.IsZero(e)
	if err != nil || !isZero {
		t.Fatalf("IsZero(nil owner) = %v, %v; want true, nil", isZero, err)
	}

	scopeField, _ := m.DimensionField("scope")
	isNull, err = scopeField.IsNull(e)
	if err != nil || isNull {
		t.Fatalf("IsNull(non-nullable scope) = %v, %v; want false, nil", isNull, err)
	}
}

func TestFieldConvert(t *testing.T) {
	m, err := Describe[*MetaEntity]()
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	owner, _ := m.DimensionField("owner")
	got, err := owner.Convert("alice")
	if err != nil {
		t.Fatalf("Convert owner: %v", err)
	}
	if got.Kind() != reflect.Ptr || got.Elem().String() != "alice" {
		t.Fatalf("owner conversion = %v", got)
	}

	scope, _ := m.DimensionField("scope")
	got, err = scope.Convert("42")
	if err != nil {
		t.Fatalf("Convert scope: %v", err)
	}
	if got.Int() != 42 {
		t.Fatalf("scope = %d, want 42", got.Int())
	}

	name, _ := m.Field("Name")
	got, err = name.Convert(int64(99))
	if err != nil {
		t.Fatalf("Convert name: %v", err)
	}
	if got.String() != "99" {
		t.Fatalf("name = %q, want 99", got.String())
	}

	enabled, _ := m.Field("Enabled")
	got, err = enabled.Convert("true")
	if err != nil {
		t.Fatalf("Convert enabled: %v", err)
	}
	if !got.Bool() {
		t.Fatal("enabled conversion should be true")
	}
}

func TestFieldConvertRejectsOverflowAndUnsupported(t *testing.T) {
	m, err := Describe[*MetaEntity]()
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	small, _ := m.Field("Small")
	if _, err := small.Convert(int64(300)); err == nil {
		t.Fatal("expected overflow error")
	}
	name, _ := m.Field("Name")
	if _, err := name.Convert(1.5); err == nil {
		t.Fatal("expected unsupported type error")
	}
	if _, err := name.Convert(nil); err == nil {
		t.Fatal("expected nil error")
	}
}
