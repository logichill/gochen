package eventsourced

import (
	"testing"

	"gochen/errors"
)

type testAggregateModule struct {
	EventSourcedSupport
	name string
}

func (m *testAggregateModule) Name() string { return m.name }

type badAggregateModule struct {
	EventSourcedSupport
}

func (m *badAggregateModule) Name() string { return "bad" }

type supportAggregateEvent struct{}

func (e *supportAggregateEvent) EventType() string { return "SupportAggregateEvent" }

type supportAggregate struct {
	*EventSourcedAggregate[int64]
}

func (a *supportAggregate) ApplySupportAggregateEvent(*supportAggregateEvent) {}

type taggedSupportAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"support_tagged"`
}

func (a *taggedSupportAggregate) ApplySupportAggregateEvent(*supportAggregateEvent) {}

type supportBadAggregate struct {
	*EventSourcedAggregate[int64]
}

func (a *supportBadAggregate) Handle1(*supportAggregateEvent) {}
func (a *supportBadAggregate) Handle2(*supportAggregateEvent) {}

func TestEventSourcedSupportAndRegisterModuleAggregates(t *testing.T) {
	t.Run("support 返回副本避免外部篡改", func(t *testing.T) {
		module := &testAggregateModule{
			EventSourcedSupport: NewEventSourcedSupport(
				Aggregate(&supportAggregate{}, "support_aggregate"),
			),
			name: "support",
		}

		first := module.MetadataRegistrations()
		first[0].AggregateType = "mutated"

		second := module.MetadataRegistrations()
		if second[0].AggregateType != "support_aggregate" {
			t.Fatalf("expected registrations to be immutable copy, got %q", second[0].AggregateType)
		}
	})

	t.Run("module aggregate 注册后运行期直接复用缓存 metadata", func(t *testing.T) {
		registry := NewMetadataRegistry()
		module := &testAggregateModule{
			EventSourcedSupport: NewEventSourcedSupport(
				Aggregate(&supportAggregate{}, "support_aggregate"),
			),
			name: "support",
		}

		if err := RegisterModuleAggregates(registry, module); err != nil {
			t.Fatalf("RegisterModuleAggregates failed: %v", err)
		}

		meta1, err := registry.Resolve(&supportAggregate{}, "support_aggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		meta2, err := registry.Register(&supportAggregate{}, "support_aggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Register failed: %v", err)
		}
		if meta1 != meta2 {
			t.Fatalf("expected module aggregate metadata to reuse the registered cache")
		}
	})

	t.Run("无效 aggregate 会带上模块名返回错误", func(t *testing.T) {
		registry := NewMetadataRegistry()
		module := &badAggregateModule{
			EventSourcedSupport: NewEventSourcedSupport(
				Aggregate(&supportBadAggregate{}, "bad_support"),
			),
		}

		err := RegisterModuleAggregates(registry, module)
		if err == nil {
			t.Fatal("expected RegisterModuleAggregates to fail")
		}
		appErr, ok := err.(*errors.AppError)
		if !ok {
			t.Fatalf("expected AppError, got %T", err)
		}
		if got := appErr.Details()["module"]; got != "bad" {
			t.Fatalf("expected module detail bad, got %#v", got)
		}
	})

	t.Run("AggregateFromTag 延迟返回 tag 提取错误", func(t *testing.T) {
		registration := AggregateFromTag(&supportBadAggregate{})
		if registration.Error == nil {
			t.Fatal("expected AggregateFromTag to capture tag error")
		}

		err := ValidateMetadataSet(registration)
		if err == nil {
			t.Fatal("expected ValidateMetadataSet to fail")
		}
		appErr, ok := err.(*errors.AppError)
		if !ok {
			t.Fatalf("expected AppError, got %T", err)
		}
		if appErr.Code() != errors.InvalidInput {
			t.Fatalf("expected InvalidInput, got %v", appErr.Code())
		}
	})

	t.Run("AggregateFromTag 成功时可注册 metadata", func(t *testing.T) {
		registry := NewMetadataRegistry()
		module := &testAggregateModule{
			EventSourcedSupport: NewEventSourcedSupport(
				AggregateFromTag(&taggedSupportAggregate{}),
			),
			name: "tagged",
		}

		if err := RegisterModuleAggregates(registry, module); err != nil {
			t.Fatalf("RegisterModuleAggregates failed: %v", err)
		}

		meta, err := registry.Resolve(&taggedSupportAggregate{}, "support_tagged")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		if meta == nil {
			t.Fatal("expected tagged aggregate metadata to be registered")
		}
	})
}
