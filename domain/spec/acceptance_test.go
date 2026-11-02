package spec

import (
	"gochen/domain/meta"
	"testing"
)

// 阶段 0 验收场景：用现有下游的真实形态验证规约代数与内存解释器。

type alifeTask struct {
	ID         int64  `entity:"id"`
	AssigneeID *int64 `dim:"owner"`
	CreatedBy  string
}

func TestAcceptanceAlifeAssigneeOrCreator(t *testing.T) {
	assignee := int64(42)
	assigned := &alifeTask{ID: 1, AssigneeID: &assignee, CreatedBy: "7"}
	created := &alifeTask{ID: 2, CreatedBy: "42"}
	other := &alifeTask{ID: 3, CreatedBy: "9"}

	// 当前 alife 的用户字段是 int64；D13 迁移后主体是 string，
	// 解释器按字段类型把 "42" 转成 int64 后比较。
	s := Or(Eq(meta.ByDimension("owner"), "42"), Eq(meta.ByField("CreatedBy"), "42"))
	i := NewInterpreter()
	for _, tc := range []struct {
		name   string
		entity any
		want   bool
	}{
		{"assignee", assigned, true},
		{"creator", created, true},
		{"other", other, false},
	} {
		ok, err := i.Match(s, tc.entity)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ok != tc.want {
			t.Fatalf("%s: Match = %v, want %v", tc.name, ok, tc.want)
		}
	}
}

type emsTask struct {
	ID             int64 `entity:"id"`
	ManagedScopeID int64 `dim:"scope"`
	LegacyTenantID int64 `dim:"tenant"`
}

func TestAcceptanceEmsScopeIsolation(t *testing.T) {
	inScope := &emsTask{ID: 1, ManagedScopeID: 2, LegacyTenantID: 9}
	outOfScope := &emsTask{ID: 2, ManagedScopeID: 3, LegacyTenantID: 9}
	i := NewInterpreter()
	ok, err := i.Match(In(meta.ByDimension("scope"), 1, 2), inScope)
	if err != nil || !ok {
		t.Fatalf("in-scope Match = %v, %v", ok, err)
	}
	ok, err = i.Match(In(meta.ByDimension("scope"), 1, 2), outOfScope)
	if err != nil || ok {
		t.Fatalf("out-of-scope Match = %v, %v", ok, err)
	}
	// 领域维度采用中性语法，资源契约承担隔离引用限制。
	if s := In(meta.ByDimension("tenant"), 9); s.Err() != nil {
		t.Fatal("valid dimension syntax must be accepted")
	}
}
