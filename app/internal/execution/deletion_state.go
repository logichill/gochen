package execution

import (
	"time"

	"gochen/domain"
	"gochen/errors"
)

// deletionState 冻结删除标记的值，不保留领域对象的可变指针。
// 普通更新只能保持该状态；状态迁移必须通过专用生命周期入口。
type deletionState struct {
	supported bool
	deleted   bool
	hasAt     bool
	at        time.Time
	hasBy     bool
	by        string
}

func captureDeletionState(entity any) deletionState {
	soft, ok := entity.(domain.ISoftDeletable)
	if !ok {
		return deletionState{}
	}
	state := deletionState{supported: true, deleted: soft.IsDeleted()}
	if at := soft.GetDeletedAt(); at != nil {
		state.hasAt = true
		state.at = *at
	}
	if attributed, ok := entity.(interface{ GetDeletedBy() *string }); ok {
		if by := attributed.GetDeletedBy(); by != nil {
			state.hasBy = true
			state.by = *by
		}
	}
	return state
}

func (before deletionState) check(entity any) error {
	after := captureDeletionState(entity)
	if before.supported != after.supported || before.deleted != after.deleted || before.hasAt != after.hasAt || before.hasBy != after.hasBy || before.by != after.by || (before.hasAt && !before.at.Equal(after.at)) {
		return errors.NewCode(errors.Forbidden, "ordinary writes cannot change deletion state; use lifecycle entry points")
	}
	return nil
}

func (state deletionState) isActive() bool { return !state.deleted && !state.hasAt && !state.hasBy }

func checkActiveCreation(entity any) error {
	state := captureDeletionState(entity)
	if !state.isActive() {
		return errors.NewCode(errors.Forbidden, "new entities must not contain deletion state; use lifecycle entry points after creation")
	}
	return nil
}
