package messaging

import (
	"context"
	"testing"

	"gochen/errors"
)

func TestTransportAlreadyStoppedRequiresSentinelCause(t *testing.T) {
	if !TransportAlreadyStopped(NewTransportAlreadyStoppedError("transport is not running")) {
		t.Fatal("expected sentinel already-stopped error to match")
	}
	if TransportAlreadyStopped(errors.NewCode(errors.Conflict, "transport cannot stop yet")) {
		t.Fatal("ordinary conflict must not be treated as already stopped")
	}
}

type stopTransportTestTransport struct {
	stopErr error
	stops   int
	stopCtx context.Context
}

func (t *stopTransportTestTransport) Stop(ctx context.Context) error {
	t.stops++
	t.stopCtx = ctx
	return t.stopErr
}

type snapshotStopTransportTestTransport struct {
	stopTransportTestTransport
	snapshotErr error
	snapshots   int
	snapshotCtx context.Context
}

func (t *snapshotStopTransportTestTransport) StopWithSnapshot(ctx context.Context) ([]IMessage, error) {
	t.snapshots++
	t.snapshotCtx = ctx
	return nil, t.snapshotErr
}

func TestStopTransportTreatsOnlyAlreadyStoppedAsSuccess(t *testing.T) {
	alreadyStopped := &stopTransportTestTransport{stopErr: NewTransportAlreadyStoppedError("transport is not running")}
	if err := StopTransport(context.Background(), alreadyStopped); err != nil {
		t.Fatalf("StopTransport already stopped error = %v", err)
	}
	if alreadyStopped.stops != 1 {
		t.Fatalf("expected Stop to be called once, got %d", alreadyStopped.stops)
	}

	conflict := &stopTransportTestTransport{stopErr: errors.NewCode(errors.Conflict, "transport cannot stop yet")}
	if err := StopTransport(context.Background(), conflict); err == nil {
		t.Fatal("expected ordinary conflict to be returned")
	}
}

func TestStopTransportNilContextUsesBackground(t *testing.T) {
	transport := &stopTransportTestTransport{}
	if err := StopTransport(nil, transport); err != nil {
		t.Fatalf("StopTransport nil context returned error: %v", err)
	}
	if transport.stops != 1 {
		t.Fatalf("expected Stop to be called once, got %d", transport.stops)
	}
	if transport.stopCtx == nil {
		t.Fatal("expected nil StopTransport context to be normalized before Stop")
	}
}

func TestStopTransportNilContextUsesBackgroundForSnapshotStop(t *testing.T) {
	transport := &snapshotStopTransportTestTransport{}
	if err := StopTransport(nil, transport); err != nil {
		t.Fatalf("StopTransport nil context returned error: %v", err)
	}
	if transport.snapshots != 1 {
		t.Fatalf("expected StopWithSnapshot to be called once, got %d", transport.snapshots)
	}
	if transport.snapshotCtx == nil {
		t.Fatal("expected nil StopTransport context to be normalized before StopWithSnapshot")
	}
}

func TestStopTransportTypedNilIsNoop(t *testing.T) {
	var transport *stopTransportTestTransport
	if err := StopTransport(context.Background(), transport); err != nil {
		t.Fatalf("StopTransport typed nil returned error: %v", err)
	}
}

func TestStopTransportPrefersSnapshotStop(t *testing.T) {
	transport := &snapshotStopTransportTestTransport{}
	if err := StopTransport(context.Background(), transport); err != nil {
		t.Fatalf("StopTransport snapshot transport returned error: %v", err)
	}
	if transport.snapshots != 1 {
		t.Fatalf("expected StopWithSnapshot to be called once, got %d", transport.snapshots)
	}
	if transport.stops != 0 {
		t.Fatalf("expected Stop not to be called when StopWithSnapshot is available, got %d", transport.stops)
	}
}

func TestStopTransportSnapshotAlreadyStoppedIsSuccess(t *testing.T) {
	transport := &snapshotStopTransportTestTransport{
		snapshotErr: NewTransportAlreadyStoppedError("transport is not running"),
	}
	if err := StopTransport(context.Background(), transport); err != nil {
		t.Fatalf("StopTransport snapshot already stopped error = %v", err)
	}
	if transport.snapshots != 1 {
		t.Fatalf("expected StopWithSnapshot to be called once, got %d", transport.snapshots)
	}
}

func TestStopTransportSnapshotOrdinaryErrorReturns(t *testing.T) {
	transport := &snapshotStopTransportTestTransport{
		snapshotErr: errors.NewCode(errors.Conflict, "transport cannot stop yet"),
	}
	if err := StopTransport(context.Background(), transport); err == nil {
		t.Fatal("expected ordinary snapshot error to be returned")
	}
}
