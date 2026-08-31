package eventsourced

import (
	"testing"

	"gochen/domain"
)

type userCreatedV1 struct{ Name string }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *userCreatedV1) EventType() string { return "UserCreated" }

type userCreatedV2 struct{ Email string }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *userCreatedV2) EventType() string { return "UserCreated" }

type panickyEvent struct{}

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *panickyEvent) EventType() string { return "PanickyEvent" }

type unhandledEvent struct{}

func (e *unhandledEvent) EventType() string { return "UnhandledEvent" }

type autoApplyAggregate struct {
	*EventSourcedAggregate[int64]

	V1Name  string
	V2Email string
}

// newAutoApplyAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*autoApplyAggregate）
func newAutoApplyAggregate(id int64) *autoApplyAggregate {
	registry := NewMetadataRegistry()
	a := &autoApplyAggregate{}
	agg, err := InitAggregate[int64](registry, a, id, "AutoApply")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyUserCreatedV1 e：实体对象。
//
// 参数：
func (a *autoApplyAggregate) ApplyUserCreatedV1(e *userCreatedV1) {
	a.V1Name = e.Name
}

// ApplyUserCreatedV2 e：实体对象。
//
// 参数：
func (a *autoApplyAggregate) ApplyUserCreatedV2(e *userCreatedV2) {
	a.V2Email = e.Email
}

// ApplyPanicky e：实体对象。
//
// 参数：
func (a *autoApplyAggregate) ApplyPanicky(e *panickyEvent) {
	panic("boom")
}

type badDuplicateHandlerAggregate struct {
	*EventSourcedAggregate[int64]
}

// newBadDuplicateHandlerAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*badDuplicateHandlerAggregate）
func newBadDuplicateHandlerAggregate(id int64) *badDuplicateHandlerAggregate {
	return &badDuplicateHandlerAggregate{
		EventSourcedAggregate: NewEventSourcedAggregate[int64](id, "BadDup"),
	}
}

// Handle1 e：实体对象。
//
// 参数：
func (a *badDuplicateHandlerAggregate) Handle1(e *userCreatedV1) {}

// Handle2 e：实体对象。
//
// 参数：
func (a *badDuplicateHandlerAggregate) Handle2(e *userCreatedV1) {}

// TestEventSourcedAggregate_AutoApply_SameEventTypeDifferentGoTypes 验证 EventSourcedAggregate AutoApply SameEventTypeDifferentGoTypes。
func TestEventSourcedAggregate_AutoApply_SameEventTypeDifferentGoTypes(t *testing.T) {
	agg := newAutoApplyAggregate(1)

	if err := agg.ApplyAndRecord(&userCreatedV1{Name: "alice"}); err != nil {
		t.Fatalf("apply v1 failed: %v", err)
	}
	if agg.V1Name != "alice" {
		t.Fatalf("expected V1Name=alice, got %s", agg.V1Name)
	}
	if agg.GetVersion() != 1 {
		t.Fatalf("expected version=1, got %d", agg.GetVersion())
	}

	if err := agg.ApplyAndRecord(&userCreatedV2{Email: "a@example.com"}); err != nil {
		t.Fatalf("apply v2 failed: %v", err)
	}
	if agg.V2Email != "a@example.com" {
		t.Fatalf("expected V2Email=a@example.com, got %s", agg.V2Email)
	}
	if agg.GetVersion() != 2 {
		t.Fatalf("expected version=2, got %d", agg.GetVersion())
	}
}

// TestEventSourcedAggregate_BindMetadata_WorksWithoutRepository 验证 EventSourcedAggregate BindMetadata WorksWithoutRepository。
func TestEventSourcedAggregate_BindMetadata_WorksWithoutRepository(t *testing.T) {
	agg := newAutoApplyAggregate(1)

	err := agg.ApplyAndRecord(&userCreatedV1{Name: "alice"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if agg.V1Name != "alice" {
		t.Fatalf("expected V1Name=alice, got %s", agg.V1Name)
	}
	if agg.GetVersion() != 1 {
		t.Fatalf("expected version=1, got %d", agg.GetVersion())
	}
}

// TestEventSourcedAggregate_HandlerPanic_IsCaught 验证 EventSourcedAggregate HandlerPanic IsCaught。
func TestEventSourcedAggregate_HandlerPanic_IsCaught(t *testing.T) {
	agg := newAutoApplyAggregate(1)
	err := agg.ApplyEvent(&panickyEvent{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if agg.GetVersion() != 0 {
		t.Fatalf("expected version unchanged on error, got %d", agg.GetVersion())
	}
}

func TestEventSourcedAggregate_MissingHandlerFails(t *testing.T) {
	agg := newAutoApplyAggregate(1)

	err := agg.ApplyEvent(&unhandledEvent{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if agg.GetVersion() != 0 {
		t.Fatalf("expected version unchanged on error, got %d", agg.GetVersion())
	}
}

// TestMetadata_ScanDuplicateHandler_Fails 验证 Metadata ScanDuplicateHandler Fails。
func TestMetadata_ScanDuplicateHandler_Fails(t *testing.T) {
	_, err := ScanMetadata(newBadDuplicateHandlerAggregate(0), "BadDup")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

var _ domain.IDomainEvent = (*userCreatedV1)(nil)
var _ domain.IDomainEvent = (*userCreatedV2)(nil)
var _ domain.IDomainEvent = (*panickyEvent)(nil)
