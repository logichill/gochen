package testkit

import (
	"context"
	"fmt"
	"reflect"

	"gochen/domain"
	"gochen/errors"
)

type memoryState[T domain.IEntity[ID], ID comparable] struct {
	entities map[ID]T
	order    []ID
}

func (r *MemoryRepository[T, ID]) withReadState(ctx context.Context, fn func(*memoryState[T, ID]) error) error {
	if transactionState, ok := r.transactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
		}
		return fn(&transaction.state)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return fn(&r.state)
}

func (r *MemoryRepository[T, ID]) withWriteState(ctx context.Context, fn func(*memoryState[T, ID]) error) error {
	if transactionState, ok := r.transactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
		}
		return fn(&transaction.state)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := fn(&r.state); err != nil {
		return err
	}
	r.revision++
	return nil
}

func cloneMemoryState[T domain.IEntity[ID], ID comparable](state memoryState[T, ID]) memoryState[T, ID] {
	cloned := memoryState[T, ID]{
		entities: make(map[ID]T, len(state.entities)),
		order:    append([]ID(nil), state.order...),
	}
	for id, entity := range state.entities {
		cloned.entities[id] = shallowClone(entity)
	}
	return cloned
}

func shallowClone[T any](value T) T {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return value
	}
	copyValue := reflect.New(rv.Elem().Type())
	copyValue.Elem().Set(rv.Elem())
	cloned, ok := copyValue.Interface().(T)
	if !ok {
		return value
	}
	return cloned
}

func validateMemoryCall[T any](ctx context.Context, entity T) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	return validateEntity(entity)
}

func validateEntity[T any](entity T) error {
	rv := reflect.ValueOf(entity)
	if !rv.IsValid() || ((rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface || rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Func) && rv.IsNil()) {
		return errors.NewCode(errors.InvalidInput, "entity is nil")
	}
	return nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

func entityNotFound[ID comparable](id ID) error {
	return errors.NewCode(errors.NotFound, "entity not found").WithContext("id", fmt.Sprint(id))
}

func duplicateEntityID[ID comparable](id ID) error {
	return errors.NewCode(errors.Conflict, "duplicate entity ID in bulk").WithContext("id", fmt.Sprint(id))
}

func removeID[ID comparable](ids []ID, target ID) []ID {
	for i, id := range ids {
		if id == target {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}

func filterIDs[ID comparable](ids []ID, removed map[ID]struct{}) []ID {
	kept := ids[:0]
	for _, id := range ids {
		if _, ok := removed[id]; !ok {
			kept = append(kept, id)
		}
	}
	return kept
}
