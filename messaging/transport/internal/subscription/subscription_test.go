package subscription

import (
	"context"
	"sync"
	"testing"

	"gochen/errors"
	"gochen/messaging"
)

type testHandler struct{ name string }

func (h testHandler) Handle(context.Context, messaging.IMessage) error { return nil }
func (h testHandler) Type() string                                     { return h.name }

type handlerFunc func(context.Context, messaging.IMessage) error

func (h handlerFunc) Handle(ctx context.Context, message messaging.IMessage) error {
	return h(ctx, message)
}

func (h handlerFunc) Type() string { return "T" }

func TestSubscribeUnsubscribesNonComparableHandlersIndependently(t *testing.T) {
	ctx := context.Background()
	var mu sync.RWMutex
	handlers := map[string][]messaging.IMessageHandler{}
	var calls [2]int
	unsubs := make([]messaging.UnsubscribeFunc, 2)
	for i := range calls {
		unsub, err := Subscribe(ctx, &mu, handlers, "T", handlerFunc(func(context.Context, messaging.IMessage) error {
			calls[i]++
			return nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		unsubs[i] = unsub
	}
	if err := unsubs[0](ctx); err != nil {
		t.Fatal(err)
	}
	for _, handler := range handlers["T"] {
		if err := handler.Handle(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != [2]int{0, 1} {
		t.Fatalf("unsubscribe removed the wrong registration: calls=%v", calls)
	}
	if err := unsubs[0](ctx); err != nil {
		t.Fatal(err)
	}
	if err := unsubs[1](ctx); err != nil {
		t.Fatal(err)
	}
	if len(handlers["T"]) != 0 {
		t.Fatal("unsubscribed handler is still registered")
	}
}

func TestSubscribe_Validation(t *testing.T) {
	var mu sync.RWMutex
	handlers := map[string][]messaging.IMessageHandler{}
	h := testHandler{name: "T"}

	if _, err := Subscribe(nil, &mu, handlers, "T", h); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil ctx, got %v", err)
	}
	if _, err := Subscribe(context.Background(), &mu, handlers, " ", h); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for blank messageType, got %v", err)
	}
	if _, err := Subscribe(context.Background(), &mu, handlers, "T", nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil handler, got %v", err)
	}
	if _, err := Subscribe(context.Background(), nil, handlers, "T", h); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil mutex, got %v", err)
	}
	if _, err := Subscribe(context.Background(), &mu, nil, "T", h); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil handlers map, got %v", err)
	}
}

func TestSubscribe_UnsubscribeIdempotent(t *testing.T) {
	var mu sync.RWMutex
	handlers := map[string][]messaging.IMessageHandler{}
	h := testHandler{name: "T"}

	unsub, err := Subscribe(context.Background(), &mu, handlers, "T", h)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got := len(handlers["T"]); got != 1 {
		t.Fatalf("expected 1 handler after subscribe, got %d", got)
	}

	if err := unsub(context.Background()); err != nil {
		t.Fatalf("first unsubscribe: %v", err)
	}
	if got := len(handlers["T"]); got != 0 {
		t.Fatalf("expected 0 handlers after unsubscribe, got %d", got)
	}

	if err := unsub(context.Background()); err != nil {
		t.Fatalf("second unsubscribe should be idempotent, got %v", err)
	}
}

func TestSubscribe_UnsubscribeValidationAndNotFound(t *testing.T) {
	var mu sync.RWMutex
	handlers := map[string][]messaging.IMessageHandler{}
	h := testHandler{name: "T"}

	unsub, err := Subscribe(context.Background(), &mu, handlers, "T", h)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := unsub(nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil unsubscribe ctx, got %v", err)
	}

	if err := Unsubscribe(context.Background(), &mu, handlers, "missing", h); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected NotFound for missing message type, got %v", err)
	}

	if err := Unsubscribe(context.Background(), &mu, handlers, "T", testHandler{name: "other"}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected NotFound for missing handler, got %v", err)
	}
}
