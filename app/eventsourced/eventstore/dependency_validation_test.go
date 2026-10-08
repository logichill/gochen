package eventstore_test

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/errors"
	"gochen/eventing/store/snapshot"
	"gochen/observe/logging"
)

type nilAdapterLogger struct{ logging.ILogger }
type failingSnapshotLoader struct{}

func (*failingSnapshotLoader) LoadSnapshot(context.Context, int64, any) (*snapshot.Snapshot[int64], error) {
	return nil, errors.NewCode(errors.Dependency, "snapshot unavailable")
}

func TestAdapterDefaultsTypedNilLoggerAndRejectsTypedNilAggregate(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	opts.Logger = (*nilAdapterLogger)(nil)
	opts.SnapshotManager = &failingSnapshotLoader{}
	adapter, err := esstore.NewDomainEventStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RestoreAggregate(context.Background(), (*testAggregate)(nil)); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	// A snapshot failure takes the logging path before falling back to the empty event stream.
	if _, err := adapter.RestoreAggregate(context.Background(), newTestAggregate(1)); err != nil {
		t.Fatal(err)
	}
}
