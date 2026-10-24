package eventsourced

import (
	"fmt"
	"sync"
	"testing"

	"gochen/domain"
	"gochen/errors"
)

type contractEvent struct{}

func (*contractEvent) EventType() string { return "contract" }

type contractAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"contract"`
	calls                         int
}

// 处理器故意不解引用事件，确保 nil 在调用前被拒绝，而非依赖 panic。
func (a *contractAggregate) ApplyContract(*contractEvent) { a.calls++ }

func (*contractAggregate) ValidateContract(*contractEvent) error {
	panic("helper must not be used for replay")
}

func (*contractAggregate) InspectContract(*contractEvent) (bool, error) {
	panic("helper must not be used for replay")
}

func TestNilEventsRejectedBeforeCallingHandler(t *testing.T) {
	for _, operation := range []string{"apply", "apply_and_record", "record"} {
		for _, input := range []struct {
			name  string
			event domain.IDomainEvent
		}{
			{"nil", nil},
			{"typed_nil", (*contractEvent)(nil)},
		} {
			t.Run(operation+"/"+input.name, func(t *testing.T) {
				a, err := New[contractAggregate, int64](NewMetadataRegistry(), 1)
				if err != nil {
					t.Fatal(err)
				}
				a.SetVersion(5)
				if err := a.ApplyAndRecord(&contractEvent{}); err != nil {
					t.Fatal(err)
				}
				switch operation {
				case "apply":
					err = a.ApplyEvent(input.event)
				case "apply_and_record":
					err = a.ApplyAndRecord(input.event)
				case "record":
					err = a.RecordUncommittedEvent(input.event)
				}
				if !errors.Is(err, errors.InvalidInput) {
					t.Fatalf("want InvalidInput, got %v", err)
				}
				if a.calls != 1 || a.GetVersion() != 6 || a.GetExpectedVersion() != 5 || len(a.GetUncommittedEvents()) != 1 {
					t.Fatal("invalid event changed aggregate state or pending events")
				}
			})
		}
	}
}

type noArgumentHandler struct{}
type extraArgumentHandler struct{}
type variadicHandler struct{}
type valueEventHandler struct{}
type interfaceEventHandler struct{}
type nonEventHandler struct{}
type multipleReturnHandler struct{}
type unnamedApplyHandler struct{}

func (*noArgumentHandler) ApplyCreated()                                 {}
func (*extraArgumentHandler) ApplyCreated(*contractEvent, int)           {}
func (*variadicHandler) ApplyCreated(...*contractEvent)                  {}
func (*valueEventHandler) ApplyCreated(contractEvent)                    {}
func (*interfaceEventHandler) ApplyCreated(domain.IDomainEvent)          {}
func (*nonEventHandler) ApplyCreated(*int)                               {}
func (*multipleReturnHandler) ApplyCreated(*contractEvent) (bool, error) { return false, nil }
func (*unnamedApplyHandler) Apply(*contractEvent)                        {}

func TestInvalidApplySignaturesFailDuringAssembly(t *testing.T) {
	for _, sample := range []any{
		&noArgumentHandler{}, &extraArgumentHandler{}, &variadicHandler{},
		&valueEventHandler{}, &interfaceEventHandler{}, &nonEventHandler{},
		&multipleReturnHandler{}, &unnamedApplyHandler{},
	} {
		t.Run(fmt.Sprintf("%T", sample), func(t *testing.T) {
			_, err := NewMetadataRegistry().Register(sample, "invalid")
			if !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("want InvalidInput, got %v", err)
			}
			var appErr *errors.AppError
			if !errors.As(err, &appErr) || appErr.Details()["handler"] == nil || appErr.Details()["aggregate_type"] != "invalid" {
				t.Fatalf("missing assembly error context: %v", err)
			}
		})
	}
}

func TestSharedMetadataOnlyCallsApplyOnCurrentInstance(t *testing.T) {
	registry := NewMetadataRegistry()
	if err := registry.RegisterSet(AggregateFromTag(&contractAggregate{})); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for id := int64(1); id <= 16; id++ {
		wg.Go(func() {
			a, err := New[contractAggregate, int64](registry, id)
			if err != nil {
				t.Error(err)
				return
			}
			for range 10 {
				if err := a.ApplyAndRecord(&contractEvent{}); err != nil {
					t.Error(err)
					return
				}
			}
			if a.calls != 10 || a.GetVersion() != 10 {
				t.Error("shared metadata changed another aggregate instance")
			}
		})
	}
	wg.Wait()
}

func TestMetadataRejectsInvalidSamplesAndEmptyTypes(t *testing.T) {
	for _, sample := range []any{nil, contractAggregate{}, new(int)} {
		if _, err := ScanMetadata(sample, "contract"); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("sample %T: want InvalidInput, got %v", sample, err)
		}
	}
	for _, aggregateType := range []string{"", "  "} {
		if _, err := ScanMetadata(&contractAggregate{}, aggregateType); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("type %q: want InvalidInput, got %v", aggregateType, err)
		}
	}
	if _, err := ScanMetadata((*contractAggregate)(nil), "contract"); err != nil {
		t.Fatalf("typed nil is a valid type-only sample: %v", err)
	}

	typedNilRegistration := Aggregate((*contractAggregate)(nil), "contract")
	if typedNilRegistration.Error != nil {
		t.Fatalf("Aggregate must accept typed nil type-only sample: %v", typedNilRegistration.Error)
	}
	if err := ValidateMetadataSet(typedNilRegistration); err != nil {
		t.Fatalf("ValidateMetadataSet must accept typed nil type-only sample: %v", err)
	}
	if err := NewMetadataRegistry().RegisterSet(typedNilRegistration); err != nil {
		t.Fatalf("RegisterSet must accept typed nil type-only sample: %v", err)
	}
}

func TestBindMetadataRejectsInvalidInstancesAndMetadata(t *testing.T) {
	metadata, err := ScanMetadata(&contractAggregate{}, "contract")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		self     any
		metadata *Metadata
	}{
		{"nil_self", nil, metadata},
		{"typed_nil_self", (*contractAggregate)(nil), metadata},
		{"nil_metadata", &contractAggregate{}, nil},
		{"zero_metadata", &contractAggregate{}, &Metadata{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewEventSourcedAggregate(1, "contract")
			if err := a.BindMetadata(tc.self, tc.metadata); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("want InvalidInput, got %v", err)
			}
			if _, err := ApplyWithMetadata(tc.self, tc.metadata, &contractEvent{}); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("direct application: want InvalidInput, got %v", err)
			}
		})
	}
	for _, evt := range []domain.IDomainEvent{nil, (*contractEvent)(nil)} {
		if _, err := ApplyWithMetadata(&contractAggregate{}, metadata, evt); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("direct application: want InvalidInput, got %v", err)
		}
	}
	for _, aggregateType := range []string{"other", ""} {
		a := NewEventSourcedAggregate(1, aggregateType)
		if err := a.BindMetadata(&contractAggregate{}, metadata); !errors.Is(err, errors.Conflict) {
			t.Fatalf("binding type %q: want Conflict, got %v", aggregateType, err)
		}
	}
}

func TestNilSampleDoesNotHideRegistrationError(t *testing.T) {
	registration := AggregateFromTag(nil)
	if err := ValidateMetadataSet(registration); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("validation: want InvalidInput, got %v", err)
	}
	if err := NewMetadataRegistry().RegisterSet(registration); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("registration: want InvalidInput, got %v", err)
	}
}
