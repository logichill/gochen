package eventsourced

import (
	"fmt"
	"testing"

	"gochen/errors"
)

type valueReceiverAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"value_receiver"`
	calls                         int
}

//nolint:staticcheck // SA4005: 有意使用值接收者写入字段，验证装配期会拒绝此类无效状态修改
func (a valueReceiverAggregate) ApplyContract(*contractEvent) { a.calls++ }

type pointerReceiverPart struct{ calls int }

func (p *pointerReceiverPart) ApplyContract(*contractEvent) { p.calls++ }

type embeddedValueReceiverAggregate struct {
	valueReceiverAggregate
}

type embeddedPointerReceiverAggregate struct {
	*EventSourcedAggregate[int64]
	*pointerReceiverPart
}

// IReceiverPart 用于验证接口嵌入的方法集约束。
type IReceiverPart interface {
	ApplyContract(*contractEvent)
}

type embeddedInterfaceReceiverAggregate struct {
	*EventSourcedAggregate[int64]
	IReceiverPart
}

type shadowedPointerReceiverAggregate struct {
	*EventSourcedAggregate[int64]
	*pointerReceiverPart
}

// 内层存在同名指针方法，不代表外层方法就是提升方法。
func (a shadowedPointerReceiverAggregate) ApplyContract(*contractEvent) {}

func TestValueMethodSetHandlersRejectedDuringAssembly(t *testing.T) {
	for _, sample := range []any{
		&valueReceiverAggregate{},
		&embeddedValueReceiverAggregate{},
		&embeddedPointerReceiverAggregate{},
		&embeddedInterfaceReceiverAggregate{},
		&shadowedPointerReceiverAggregate{},
	} {
		t.Run(fmt.Sprintf("%T", sample), func(t *testing.T) {
			_, scanErr := ScanMetadata(sample, "receiver_test")
			_, registerErr := NewMetadataRegistry().Register(sample, "receiver_test")
			validateErr := ValidateMetadataSet(Aggregate(sample, "receiver_test"))
			for _, err := range []error{scanErr, registerErr, validateErr} {
				if !errors.Is(err, errors.InvalidInput) {
					t.Fatalf("want InvalidInput, got %v", err)
				}
				var appErr *errors.AppError
				if !errors.As(err, &appErr) || appErr.Details()["handler"] != "ApplyContract" {
					t.Fatalf("missing handler context: %v", err)
				}
			}
		})
	}
	if a, err := New[valueReceiverAggregate, int64](NewMetadataRegistry(), 1); a != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("constructor must reject value receiver before events can be applied: aggregate=%v error=%v", a, err)
	}
}

type valueEmbeddedPointerAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"value_embedded_pointer"`
	pointerReceiverPart
}

type forwardedPointerAggregate struct {
	*EventSourcedAggregate[int64] `aggregate:"forwarded_pointer"`
	*pointerReceiverPart
}

func (a *forwardedPointerAggregate) ApplyContract(e *contractEvent) {
	a.pointerReceiverPart.ApplyContract(e)
}

func TestEmbeddedPointerHandlersUpdateOriginalState(t *testing.T) {
	registry := NewMetadataRegistry()
	if err := registry.RegisterSet(
		AggregateFromTag(&valueEmbeddedPointerAggregate{}),
		AggregateFromTag(&forwardedPointerAggregate{}),
	); err != nil {
		t.Fatal(err)
	}
	valueEmbedded, err := New[valueEmbeddedPointerAggregate, int64](registry, 1)
	if err != nil {
		t.Fatal(err)
	}
	forwarded, err := New[forwardedPointerAggregate, int64](registry, 2)
	if err != nil {
		t.Fatal(err)
	}
	forwarded.pointerReceiverPart = &pointerReceiverPart{}
	for _, tc := range []struct {
		name  string
		base  *EventSourcedAggregate[int64]
		calls *int
	}{
		{"value_embedding", valueEmbedded.EventSourcedAggregate, &valueEmbedded.calls},
		{"explicit_forwarding", forwarded.EventSourcedAggregate, &forwarded.calls},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.base.ApplyAndRecord(&contractEvent{}); err != nil {
				t.Fatal(err)
			}
			if *tc.calls != 1 || tc.base.GetVersion() != 1 || len(tc.base.GetUncommittedEvents()) != 1 {
				t.Fatal("handler must update original state together with version and pending event")
			}
		})
	}
}
