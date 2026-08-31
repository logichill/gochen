package eventsourced

import (
	"testing"

	"gochen/domain"
)

// ─── 测试用聚合类型 ────────────────────────────────────────────────

// taggedPtrAggregate 指针嵌入 + aggregate tag。
type taggedPtrAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"tagged_ptr"`
	Value                         int
}

// taggedValueAggregate 值嵌入 + aggregate tag。
type taggedValueAggregate struct {
	EventSourcedAggregate[int64] `aggregate:"tagged_value"`
	Value                        int
}

// taggedOptionsAggregate tag 含逗号选项。
type taggedOptionsAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"tagged_opts,omitempty"`
	Value                         int
}

// missingTagAggregate 嵌入了 ES，但没有 aggregate tag。
type missingTagAggregate struct {
	*EventSourcedAggregate[int64]
	Value int
}

// emptyTagAggregate tag 值为空。
type emptyTagAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:""`
	Value                         int
}

// dashTagAggregate tag 值为 "-"。
type dashTagAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"-"`
	Value                         int
}

// noESAggregate 没有嵌入 EventSourcedAggregate。
type noESAggregate struct {
	ID    int64
	Value int
}

// stringIDAggregate 使用 string 类型 ID。
type stringIDAggregate struct {
	*EventSourcedAggregate[string] `aggregate:"string_id_agg"`
	Name                           string
}

// ─── tag event（供 auto apply 测试用）────────────────────────────

type tagTestEvent struct {
	Amount int
}

func (e *tagTestEvent) EventType() string { return "tag_test_event" }

var _ domain.IDomainEvent = (*tagTestEvent)(nil)

// Apply 方法。
func (a *taggedPtrAggregate) ApplyTagTest(e *tagTestEvent) {
	a.Value += e.Amount
}

func (a *taggedValueAggregate) ApplyTagTest(e *tagTestEvent) {
	a.Value += e.Amount
}

func (a *stringIDAggregate) ApplyTagTest(e *tagTestEvent) {
	a.Name = "applied"
}

// ─── Tests ─────────────────────────────────────────────────────────

// TestResolveAggregateType_PtrEmbed 验证 ResolveAggregateType PtrEmbed。
func TestResolveAggregateType_PtrEmbed(t *testing.T) {
	got, err := ResolveAggregateType(&taggedPtrAggregate{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tagged_ptr" {
		t.Fatalf("expected 'tagged_ptr', got %q", got)
	}
}

// TestResolveAggregateType_ValueEmbed 验证 ResolveAggregateType ValueEmbed。
func TestResolveAggregateType_ValueEmbed(t *testing.T) {
	got, err := ResolveAggregateType(&taggedValueAggregate{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tagged_value" {
		t.Fatalf("expected 'tagged_value', got %q", got)
	}
}

// TestResolveAggregateType_WithOptions 验证 ResolveAggregateType WithOptions。
func TestResolveAggregateType_WithOptions(t *testing.T) {
	got, err := ResolveAggregateType(&taggedOptionsAggregate{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tagged_opts" {
		t.Fatalf("expected 'tagged_opts', got %q", got)
	}
}

// TestResolveAggregateType_MissingTag 验证 ResolveAggregateType MissingTag。
func TestResolveAggregateType_MissingTag(t *testing.T) {
	_, err := ResolveAggregateType(&missingTagAggregate{})
	if err == nil {
		t.Fatal("expected error for missing tag, got nil")
	}
}

// TestResolveAggregateType_EmptyTag 验证 ResolveAggregateType EmptyTag。
func TestResolveAggregateType_EmptyTag(t *testing.T) {
	_, err := ResolveAggregateType(&emptyTagAggregate{})
	if err == nil {
		t.Fatal("expected error for empty tag, got nil")
	}
}

// TestResolveAggregateType_DashTag 验证 ResolveAggregateType DashTag。
func TestResolveAggregateType_DashTag(t *testing.T) {
	_, err := ResolveAggregateType(&dashTagAggregate{})
	if err == nil {
		t.Fatal("expected error for '-' tag, got nil")
	}
}

// TestResolveAggregateType_NoESField 验证 ResolveAggregateType NoESField。
func TestResolveAggregateType_NoESField(t *testing.T) {
	_, err := ResolveAggregateType(&noESAggregate{})
	if err == nil {
		t.Fatal("expected error for struct without ES field, got nil")
	}
}

// TestResolveAggregateType_Nil 验证 ResolveAggregateType Nil。
func TestResolveAggregateType_Nil(t *testing.T) {
	_, err := ResolveAggregateType(nil)
	if err == nil {
		t.Fatal("expected error for nil sample, got nil")
	}
}

// TestResolveAggregateType_NotStruct 验证 ResolveAggregateType NotStruct。
func TestResolveAggregateType_NotStruct(t *testing.T) {
	x := 42
	_, err := ResolveAggregateType(&x)
	if err == nil {
		t.Fatal("expected error for non-struct, got nil")
	}
}

// TestResolveAggregateType_StringID 验证 ResolveAggregateType StringID。
func TestResolveAggregateType_StringID(t *testing.T) {
	got, err := ResolveAggregateType(&stringIDAggregate{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "string_id_agg" {
		t.Fatalf("expected 'string_id_agg', got %q", got)
	}
}

// ─── InitAggregateFromTag tests ────────────────────────────────────

// TestInitAggregateFromTag_PtrEmbed 验证 InitAggregateFromTag PtrEmbed。
func TestInitAggregateFromTag_PtrEmbed(t *testing.T) {
	registry := NewMetadataRegistry()
	a := &taggedPtrAggregate{}
	agg, err := InitAggregateFromTag[int64](registry, a, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.EventSourcedAggregate = agg

	if agg.GetAggregateType() != "tagged_ptr" {
		t.Fatalf("expected aggregateType 'tagged_ptr', got %q", agg.GetAggregateType())
	}
	if agg.GetID() != 42 {
		t.Fatalf("expected ID 42, got %v", agg.GetID())
	}

	// 验证事件应用
	if err := a.ApplyAndRecord(&tagTestEvent{Amount: 10}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
	if a.Value != 10 {
		t.Fatalf("expected Value=10, got %d", a.Value)
	}
}

// TestInitAggregateFromTag_ValueEmbed 验证 InitAggregateFromTag ValueEmbed。
func TestInitAggregateFromTag_ValueEmbed(t *testing.T) {
	registry := NewMetadataRegistry()
	a := &taggedValueAggregate{}
	agg, err := InitAggregateFromTag[int64](registry, a, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.EventSourcedAggregate = *agg

	if agg.GetAggregateType() != "tagged_value" {
		t.Fatalf("expected aggregateType 'tagged_value', got %q", agg.GetAggregateType())
	}
}

// TestInitAggregateFromTag_StringID 验证 InitAggregateFromTag StringID。
func TestInitAggregateFromTag_StringID(t *testing.T) {
	registry := NewMetadataRegistry()
	a := &stringIDAggregate{}
	agg, err := InitAggregateFromTag[string](registry, a, "abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.EventSourcedAggregate = agg

	if agg.GetAggregateType() != "string_id_agg" {
		t.Fatalf("expected aggregateType 'string_id_agg', got %q", agg.GetAggregateType())
	}
	if agg.GetID() != "abc-123" {
		t.Fatalf("expected ID 'abc-123', got %q", agg.GetID())
	}

	// 验证事件应用
	if err := a.ApplyAndRecord(&tagTestEvent{Amount: 1}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
	if a.Name != "applied" {
		t.Fatalf("expected Name='applied', got %q", a.Name)
	}
}

// TestInitAggregateFromTag_ErrorNoTag 验证 InitAggregateFromTag ErrorNoTag。
func TestInitAggregateFromTag_ErrorNoTag(t *testing.T) {
	registry := NewMetadataRegistry()
	a := &missingTagAggregate{}
	_, err := InitAggregateFromTag[int64](registry, a, 1)
	if err == nil {
		t.Fatal("expected error for missing tag, got nil")
	}
}

// TestInitAggregateFromTag_MetadataReusedFromRegistry 验证 InitAggregateFromTag 复用已注册 metadata。
func TestInitAggregateFromTag_MetadataReusedFromRegistry(t *testing.T) {
	registry := NewMetadataRegistry()
	// 先通过显式注册路径注册一次
	_, err := registry.Register(&taggedPtrAggregate{}, "tagged_ptr")
	if err != nil {
		t.Fatalf("pre-register failed: %v", err)
	}

	// 再通过 tag 路径创建，应直接复用
	a := &taggedPtrAggregate{}
	agg, err := InitAggregateFromTag[int64](registry, a, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.EventSourcedAggregate = agg

	if agg.GetAggregateType() != "tagged_ptr" {
		t.Fatalf("expected 'tagged_ptr', got %q", agg.GetAggregateType())
	}
}

// ─── New tests ──────────────────────────────────────────────────────

// TestNew_PtrEmbed 验证 New 指针嵌入聚合。
func TestNew_PtrEmbed(t *testing.T) {
	registry := NewMetadataRegistry()
	a, err := New[taggedPtrAggregate, int64](registry, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.EventSourcedAggregate == nil {
		t.Fatal("expected EventSourcedAggregate to be initialized, got nil")
	}
	if a.GetAggregateType() != "tagged_ptr" {
		t.Fatalf("expected aggregateType 'tagged_ptr', got %q", a.GetAggregateType())
	}
	if a.GetID() != 42 {
		t.Fatalf("expected ID 42, got %v", a.GetID())
	}

	// 验证事件可以正常应用
	if err := a.ApplyAndRecord(&tagTestEvent{Amount: 10}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
	if a.Value != 10 {
		t.Fatalf("expected Value=10, got %d", a.Value)
	}
}

// TestNew_ValueEmbed 验证 New 值嵌入聚合。
func TestNew_ValueEmbed(t *testing.T) {
	registry := NewMetadataRegistry()
	a, err := New[taggedValueAggregate, int64](registry, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.GetAggregateType() != "tagged_value" {
		t.Fatalf("expected aggregateType 'tagged_value', got %q", a.GetAggregateType())
	}
	if a.GetID() != 99 {
		t.Fatalf("expected ID 99, got %v", a.GetID())
	}

	// 验证事件可以正常应用
	if err := a.ApplyAndRecord(&tagTestEvent{Amount: 5}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
	if a.Value != 5 {
		t.Fatalf("expected Value=5, got %d", a.Value)
	}
}

// TestNew_StringID 验证 New string ID 泛型。
func TestNew_StringID(t *testing.T) {
	registry := NewMetadataRegistry()
	a, err := New[stringIDAggregate, string](registry, "abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.GetAggregateType() != "string_id_agg" {
		t.Fatalf("expected aggregateType 'string_id_agg', got %q", a.GetAggregateType())
	}
	if a.GetID() != "abc-123" {
		t.Fatalf("expected ID 'abc-123', got %q", a.GetID())
	}

	// 验证事件可以正常应用
	if err := a.ApplyAndRecord(&tagTestEvent{Amount: 1}); err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}
	if a.Name != "applied" {
		t.Fatalf("expected Name='applied', got %q", a.Name)
	}
}

// TestNew_ErrorMissingTag 验证 New 无 tag 报错。
func TestNew_ErrorMissingTag(t *testing.T) {
	registry := NewMetadataRegistry()
	_, err := New[missingTagAggregate, int64](registry, 1)
	if err == nil {
		t.Fatal("expected error for missing tag, got nil")
	}
}

// TestNew_ErrorNoESField 验证 New 无 ES 字段报错。
func TestNew_ErrorNoESField(t *testing.T) {
	registry := NewMetadataRegistry()
	_, err := New[noESAggregate, int64](registry, 1)
	if err == nil {
		t.Fatal("expected error for struct without ES field, got nil")
	}
}

// TestNew_FieldsSettableAfterConstruction 验证 New 创建后可以设置业务字段。
func TestNew_FieldsSettableAfterConstruction(t *testing.T) {
	registry := NewMetadataRegistry()
	a, err := New[taggedPtrAggregate, int64](registry, 100)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	a.Value = 42

	if a.Value != 42 {
		t.Fatalf("expected Value=42, got %d", a.Value)
	}
	if a.GetID() != 100 {
		t.Fatalf("expected ID 100, got %v", a.GetID())
	}
}
