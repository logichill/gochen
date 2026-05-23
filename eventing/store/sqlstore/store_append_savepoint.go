package sqlstore

import (
	"context"
	"fmt"
	"sync/atomic"

	"gochen/db"
	"gochen/errors"
)

var appendSavepointSeq uint64

type savepointScope struct {
	tx     db.ISavepointTransaction
	name   string
	active bool
}

func beginSavepointScope(ctx context.Context, database db.IDatabase, prefix string) (*savepointScope, error) {
	tx, ok := database.(db.ISavepointTransaction)
	if !ok {
		return nil, nil
	}
	if capability, ok := database.(db.ISavepointCapabilityProvider); ok && !capability.SupportsSavepoints() {
		return nil, nil
	}

	name := fmt.Sprintf("gochen_%s_%d", prefix, atomic.AddUint64(&appendSavepointSeq, 1))
	if err := tx.CreateSavepoint(ctx, name); err != nil {
		return nil, err
	}
	return &savepointScope{tx: tx, name: name, active: true}, nil
}

func (s *savepointScope) release(ctx context.Context) error {
	if s == nil || !s.active {
		return nil
	}
	if err := s.tx.ReleaseSavepoint(ctx, s.name); err != nil {
		return err
	}
	s.active = false
	return nil
}

func (s *savepointScope) rollback(ctx context.Context) error {
	if s == nil || !s.active {
		return nil
	}
	if err := s.tx.RollbackToSavepoint(ctx, s.name); err != nil {
		return err
	}
	if err := s.tx.ReleaseSavepoint(ctx, s.name); err != nil {
		return err
	}
	s.active = false
	return nil
}

func executeRecoverableStatement(ctx context.Context, database db.IDatabase, prefix string, fn func() error) error {
	scope, err := beginSavepointScope(ctx, database, prefix)
	if err != nil {
		return errors.NewCodeWithCause(errors.Database, "create savepoint failed", err)
	}

	if err := fn(); err != nil {
		if scope != nil {
			if rbErr := scope.rollback(ctx); rbErr != nil {
				return errors.NewCodeWithCause(errors.Database, "rollback savepoint failed", rbErr).
					WithContext("statement_error", err.Error())
			}
		}
		return err
	}

	if scope != nil {
		if err := scope.release(ctx); err != nil {
			return errors.NewCodeWithCause(errors.Database, "release savepoint failed", err)
		}
	}
	return nil
}
