package outbox

import (
	"sync"
	"sync/atomic"
)

type oneShotOutboxRepo struct {
	mu sync.Mutex

	entries []OutboxEntry[int64]
	pubs    int
	fails   int
}

func newOneShotOutboxRepo(entries []OutboxEntry[int64]) *oneShotOutboxRepo {
	cp := append([]OutboxEntry[int64](nil), entries...)
	return &oneShotOutboxRepo{entries: cp}
}

type recordingBatchOps struct {
	mu sync.Mutex

	published            [][]ClaimedEntry
	failed               [][]FailedEntry
	markPublishedErr     error
	publishedErr         []error
	publishedHasDeadline []bool
	publishedCh          chan error
	publishedSnapshotCh  chan markBatchContextSnapshot
}

type cancelDuringPublishedBatchOps struct {
	started chan struct{}
	release chan struct{}
}

type cancelDuringFailedBatchOps struct {
	started chan struct{}
	release chan struct{}
}

type markBatchContextSnapshot struct {
	err         error
	hasDeadline bool
}

type recordingAtomicDLQ struct {
	MockDLQRepository

	mu     sync.Mutex
	atomic []OutboxEntry[int64]
	err    error
}

type blockingMarkPublishedRepo struct {
	MockOutboxRepository
	calls atomic.Int64
}
