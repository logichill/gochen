package main

import (
	"context"
	"errors"
	"log"
	"time"

	"gochen/messaging"
	"gochen/messaging/deadletter"
	deadlettermemory "gochen/messaging/deadletter/memory"
	"gochen/messaging/transport/memory"
)

type failingHandler struct{}

func (failingHandler) Type() string {
	return "invoice-handler"
}

func (failingHandler) Handle(context.Context, messaging.IMessage) error {
	return errors.New("invoice provider unavailable")
}

func main() {
	ctx := context.Background()
	transport := memory.NewMemoryTransport(16, 1)
	sink := deadlettermemory.NewSink()
	transport.SetDeadLetterSink(sink)
	must(transport.Start(ctx))

	unsubscribe, err := transport.Subscribe(ctx, "invoice.requested", failingHandler{})
	must(err)
	must(transport.Publish(ctx, messaging.NewMessage(
		"message-1",
		messaging.KindCommand,
		"invoice.requested",
		map[string]any{"invoice_id": "invoice-42"},
	)))

	entry := waitForDeadLetter(sink)
	log.Printf("dead letter: message=%s handler=%s error=%v",
		entry.Message.GetID(),
		entry.HandlerType,
		entry.Err,
	)

	must(unsubscribe(context.Background()))
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	must(transport.Stop(stopCtx))
}

func waitForDeadLetter(sink *deadlettermemory.Sink) deadletter.Entry {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries := sink.Entries()
		if len(entries) > 0 {
			return entries[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	log.Fatal("dead letter was not recorded")
	return deadletter.Entry{}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
