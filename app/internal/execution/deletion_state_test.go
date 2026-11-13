package execution

import (
	"testing"
	"time"

	"gochen/domain"
	"gochen/errors"
)

type independentDeletionState struct {
	domain.DeletionState
	deleted bool
}

func (s *independentDeletionState) IsDeleted() bool { return s.deleted }

func TestDeletionStateFreezesPointerValuesAndIndependentFlag(t *testing.T) {
	for _, mutation := range []string{"at-pointer", "by-pointer", "clear-at", "clear-by", "flag"} {
		t.Run(mutation, func(t *testing.T) {
			at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			by := "alice"
			entity := &independentDeletionState{DeletionState: domain.DeletionState{DeletedAt: &at, DeletedBy: &by}}
			frozen := captureDeletionState(entity)
			switch mutation {
			case "at-pointer":
				*entity.DeletedAt = at.Add(time.Hour)
			case "by-pointer":
				*entity.DeletedBy = "bob"
			case "clear-at":
				entity.DeletedAt = nil
			case "clear-by":
				entity.DeletedBy = nil
			case "flag":
				entity.deleted = true
			}
			if err := frozen.check(entity); !errors.Is(err, errors.Forbidden) {
				t.Fatalf("changed %s accepted: %v", mutation, err)
			}
		})
	}
}

func TestDeletionStateAllowsEquivalentIndependentCopies(t *testing.T) {
	at := time.Now()
	by := "alice"
	entity := &independentDeletionState{DeletionState: domain.DeletionState{DeletedAt: &at, DeletedBy: &by}}
	frozen := captureDeletionState(entity)
	otherAt, otherBy := at, by
	entity.DeletedAt = &otherAt
	entity.DeletedBy = &otherBy
	if err := frozen.check(entity); err != nil {
		t.Fatalf("equivalent state rejected: %v", err)
	}
}
