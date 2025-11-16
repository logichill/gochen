package sync

import (
	"context"
	"testing"

	msg "gochen/messaging"
)

type incHandler struct{ n *int }

func (h incHandler) Handle(ctx context.Context, m msg.IMessage) error { *h.n++; return nil }
func (h incHandler) Type() string                                     { return "inc" }

func TestSyncTransport_PublishFlow(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer tpt.Close()

	var c int
	if err := tpt.Subscribe("T", incHandler{n: &c}); err != nil {
		t.Fatalf("sub: %v", err)
	}

	msg := &msg.Message{ID: "1", Type: "T"}
	if err := tpt.Publish(context.Background(), msg); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if c != 1 {
		t.Fatalf("expected 1, got %d", c)
	}
}

func TestSyncTransport_NotRunning(t *testing.T) {
	tpt := NewSyncTransport()
	if err := tpt.Publish(context.Background(), &msg.Message{ID: "x", Type: "T"}); err == nil {
		t.Fatalf("expected error when not running")
	}
}
