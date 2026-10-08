package workflow

import (
	"context"

	"sync"

	"gochen/errors"
)

type nilContextStore struct {
	called bool
}

func (s *nilContextStore) markCalled() error {
	s.called = true
	return errors.New("store should not be called")
}

func (s *nilContextStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	return nil, s.markCalled()
}

func (s *nilContextStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return s.markCalled()
}

func (s *nilContextStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	return s.markCalled()
}

func (s *nilContextStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, s.markCalled()
}

func (s *nilContextStore) Save(ctx context.Context, st *State) error {
	return s.markCalled()
}

func (s *nilContextStore) Delete(ctx context.Context, id ID) error {
	return s.markCalled()
}

type failingStore struct{}

func (failingStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	return nil, errors.New("store get definition failed")
}

func (failingStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return errors.New("store save definition failed")
}

func (failingStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	return nil
}

func (failingStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, errors.New("store get failed")
}

func (failingStore) Save(ctx context.Context, st *State) error {
	return errors.New("store save failed")
}

func (failingStore) Delete(ctx context.Context, id ID) error { return nil }

type definitionSaveFailStore struct {
	*MemoryStore
}

func (s *definitionSaveFailStore) SaveDefinition(ctx context.Context, def *Definition) error {
	return errors.New("store save definition failed")
}

type instanceSaveFailStore struct {
	*MemoryStore
}

func (s *instanceSaveFailStore) Save(ctx context.Context, st *State) error {
	return errors.New("store save failed")
}

func (s *instanceSaveFailStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	return errors.New("store save failed")
}

type saveOnlyDefinitionStore struct {
	getDefinitionCalls int
	saved              *Definition
}

func (s *saveOnlyDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	s.getDefinitionCalls++
	return nil, errors.New("store get definition should not be called")
}

func (s *saveOnlyDefinitionStore) SaveDefinition(ctx context.Context, def *Definition) error {
	s.saved = cloneDefinition(def)
	return nil
}

func (s *saveOnlyDefinitionStore) DeleteDefinition(ctx context.Context, id string, version uint32) error {
	return nil
}

func (s *saveOnlyDefinitionStore) Get(ctx context.Context, id ID) (*State, error) {
	return nil, nil
}

func (s *saveOnlyDefinitionStore) Save(ctx context.Context, st *State) error {
	return nil
}

func (s *saveOnlyDefinitionStore) Delete(ctx context.Context, id ID) error {
	return nil
}

type blockingMissingDefinitionStore struct {
	*MemoryStore
	firstMissingGetStarted  chan struct{}
	secondMissingGetStarted chan struct{}
	releaseFirstMissingGet  chan struct{}
	mu                      sync.Mutex
	firstSeen               bool
}

func newBlockingMissingDefinitionStore() *blockingMissingDefinitionStore {
	return &blockingMissingDefinitionStore{
		MemoryStore:             NewMemoryStore(),
		firstMissingGetStarted:  make(chan struct{}),
		secondMissingGetStarted: make(chan struct{}),
		releaseFirstMissingGet:  make(chan struct{}),
	}
}

func (s *blockingMissingDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	def, err := s.MemoryStore.GetDefinition(ctx, id, version)
	if err != nil || def != nil {
		return def, err
	}

	s.mu.Lock()
	first := !s.firstSeen
	if first {
		s.firstSeen = true
	}
	s.mu.Unlock()

	if first {
		close(s.firstMissingGetStarted)
		<-s.releaseFirstMissingGet
		return nil, nil
	}

	close(s.secondMissingGetStarted)
	return nil, nil
}

type notFoundDefinitionStore struct {
	*MemoryStore
}

func (s notFoundDefinitionStore) GetDefinition(ctx context.Context, id string, version uint32) (*Definition, error) {
	def, err := s.MemoryStore.GetDefinition(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, errors.NewCode(errors.NotFound, "workflow definition not found")
	}
	return def, nil
}

type blockingVersionStore struct {
	*MemoryStore
	ready   chan struct{}
	barrier chan struct{}
}

func newBlockingVersionStore() *blockingVersionStore {
	return &blockingVersionStore{
		MemoryStore: NewMemoryStore(),
		ready:       make(chan struct{}, 2),
		barrier:     make(chan struct{}),
	}
}

func (s *blockingVersionStore) SaveIfVersion(ctx context.Context, st *State, expectedVersion uint64) error {
	if expectedVersion > 0 {
		s.ready <- struct{}{}
		<-s.barrier
	}
	return s.MemoryStore.SaveIfVersion(ctx, st, expectedVersion)
}
