package subscriber

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/observe/logging"
)

type nilSubscriberLogger struct{ logging.ILogger }
type nilSubscriberBus struct{ bus.IEventBus }
type nilProjectionRegistrar struct {
	projection.IProjectionRegistrar
}

func TestSubscribersDefaultTypedNilLoggerAndRejectTypedNilEvents(t *testing.T) {
	failure := errors.NewCode(errors.Dependency, "handler failed")
	handle := func(context.Context, int) error { return failure }
	decode := func(eventing.IEvent) (int, error) { return 1, nil }
	handler, err := NewEventSourcedTypedHandler(EventSourcedHandlerOption[int]{EventType: "test", Handle: handle, Decoder: decode, Logger: (*nilSubscriberLogger)(nil)})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := NewEventSourcedProjection(EventSourcedProjectionOption[int, int64]{Name: "test", EventTypes: []string{"test"}, Handle: handle, Decoder: decode, Logger: (*nilSubscriberLogger)(nil)})
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []func(context.Context, eventing.IEvent) error{handler.HandleEvent, proj.Handle} {
		if err := run(context.Background(), (*eventing.Event[int64])(nil)); !errors.Is(err, errors.InvalidInput) {
			t.Fatal(err)
		}
		if err := run(context.Background(), &eventing.Event[int64]{}); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
}

func TestRegistrarRejectsTypedNilDependencies(t *testing.T) {
	registrar := NewEventSourcedAutoRegistrar((*nilSubscriberBus)(nil), (*nilProjectionRegistrar)(nil))
	if _, err := registrar.RegisterHandlers(context.Background()); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	if _, err := registrar.RegisterProjections(context.Background()); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
}
