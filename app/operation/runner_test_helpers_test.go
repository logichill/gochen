package operation

import (
	"context"
	"testing"
	"time"

	gerrors "gochen/errors"
)

type idempotencyOnlyStore struct {
	inner *MemoryStore
}

type baseOnlyStore struct {
	inner *MemoryStore
}

type conflictOnPutStore struct {
	*MemoryStore
	conflicted map[string]bool
}

type lateConflictOnPutStore struct {
	existing *Result
	getCalls int
	putCalls int
}

type failOnceOnPutStore struct {
	*MemoryStore
	failed bool
}

type failingStore struct{}

type staleReservationStore struct {
	*MemoryStore
	reserveCalls int
}

func newConflictOnPutStore() *conflictOnPutStore {
	return &conflictOnPutStore{
		MemoryStore: NewMemoryStore(),
		conflicted:  make(map[string]bool),
	}
}

func newFailOnceOnPutStore() *failOnceOnPutStore {
	return &failOnceOnPutStore{MemoryStore: NewMemoryStore()}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, timeoutMessage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(timeoutMessage)
	}
}

func (s *conflictOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if key != "" && !s.conflicted[key] {
		s.conflicted[key] = true
		return gerrors.NewCode(gerrors.Conflict, "simulated put conflict")
	}
	return s.MemoryStore.PutWithIdempotencyKey(ctx, key, result)
}

func (s *lateConflictOnPutStore) Get(ctx context.Context, id string) (*Result, error) {
	return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
}

func (s *lateConflictOnPutStore) Put(ctx context.Context, result *Result) error {
	return gerrors.NewCode(gerrors.Database, "unexpected non-idempotent put")
}

func (s *lateConflictOnPutStore) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *lateConflictOnPutStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	s.getCalls++
	if s.getCalls == 1 {
		return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
	}
	return CloneResult(s.existing), nil
}

func (s *lateConflictOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	s.putCalls++
	return gerrors.NewCode(gerrors.Conflict, "simulated late put conflict")
}

func (s *failOnceOnPutStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	if key != "" && !s.failed {
		s.failed = true
		return gerrors.NewCode(gerrors.Database, "simulated store failure")
	}
	return s.MemoryStore.PutWithIdempotencyKey(ctx, key, result)
}

func (s failingStore) Get(ctx context.Context, id string) (*Result, error) {
	return nil, gerrors.NewCode(gerrors.NotFound, "operation not found")
}

func (s failingStore) Put(ctx context.Context, result *Result) error {
	return gerrors.NewCode(gerrors.Database, "store failed")
}

func (s failingStore) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *staleReservationStore) ReserveIdempotencyKey(ctx context.Context, key string, operationID string) error {
	s.reserveCalls++
	if s.reserveCalls == 1 {
		return gerrors.NewCode(gerrors.Conflict, "stale reservation")
	}
	return s.MemoryStore.ReserveIdempotencyKey(ctx, key, operationID)
}

func (s idempotencyOnlyStore) Get(ctx context.Context, id string) (*Result, error) {
	return s.inner.Get(ctx, id)
}

func (s idempotencyOnlyStore) Put(ctx context.Context, result *Result) error {
	return s.inner.Put(ctx, result)
}

func (s idempotencyOnlyStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s idempotencyOnlyStore) GetByIdempotencyKey(ctx context.Context, key string) (*Result, error) {
	return s.inner.GetByIdempotencyKey(ctx, key)
}

func (s idempotencyOnlyStore) PutWithIdempotencyKey(ctx context.Context, key string, result *Result) error {
	return s.inner.PutWithIdempotencyKey(ctx, key, result)
}

func (s *baseOnlyStore) Get(ctx context.Context, id string) (*Result, error) {
	return s.inner.Get(ctx, id)
}

func (s *baseOnlyStore) Put(ctx context.Context, result *Result) error {
	return s.inner.Put(ctx, result)
}

func (s *baseOnlyStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
