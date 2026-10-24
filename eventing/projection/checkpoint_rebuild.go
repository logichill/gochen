package projection

import (
	"context"
)

// saveRebuildCheckpoint 在重建结束时把游标无条件对齐到重建结果。
//
// 重建会把读模型重放到 events 的最终位置，该位置可能低于已持久化游标；若仍走只前进的
// Save，会出现“读模型已回退、持久游标仍停在旧位置”，恢复时跳过区间事件。故优先使用
// 支持 ForceSave 的 store；不支持时退回“先删后存”，使后续 Save 不再受旧位置限制。
func saveRebuildCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if store == nil || checkpoint == nil {
		return nil
	}
	return forceSaveCheckpoint(ctx, store, checkpoint)
}

func forceSaveCheckpoint(ctx context.Context, store ICheckpointStore, checkpoint *Checkpoint) error {
	if forcer, ok := store.(ICheckpointForceSaver); ok {
		return forcer.ForceSave(ctx, checkpoint)
	}
	// 备份原检查点，防止先删后存降级流程中 Save 失败导致原检查点数据彻底灭失 (F37)
	backup, loadErr := store.Load(ctx, checkpoint.ProjectionName)
	if err := store.Delete(ctx, checkpoint.ProjectionName); err != nil {
		return err
	}
	if err := store.Save(ctx, checkpoint); err != nil {
		if loadErr == nil && backup != nil {
			// 使用 WithoutCancel：裸 context.Background() 会丢失事务会话与
			// 租户/追踪变量，事务型检查点存储（ICheckpointTxSessionRequirer）
			// 会因无事务而回写失败，导致原检查点彻底灭失。
			_ = store.Save(context.WithoutCancel(ctx), backup)
		}
		return err
	}
	return nil
}

type rebuildCheckpointStore struct {
	inner ICheckpointStore
}

func newRebuildCheckpointStore(store ICheckpointStore) ICheckpointStore {
	if store == nil {
		return nil
	}
	return rebuildCheckpointStore{inner: store}
}

func (s rebuildCheckpointStore) Load(ctx context.Context, projectionName string) (*Checkpoint, error) {
	return s.inner.Load(ctx, projectionName)
}

func (s rebuildCheckpointStore) Save(ctx context.Context, checkpoint *Checkpoint) error {
	return forceSaveCheckpoint(ctx, s.inner, checkpoint)
}

func (s rebuildCheckpointStore) Delete(ctx context.Context, projectionName string) error {
	return s.inner.Delete(ctx, projectionName)
}

func (s rebuildCheckpointStore) ForceSave(ctx context.Context, checkpoint *Checkpoint) error {
	return forceSaveCheckpoint(ctx, s.inner, checkpoint)
}

func (s rebuildCheckpointStore) RequiresORMTxSession() bool {
	requirer, ok := s.inner.(ICheckpointTxSessionRequirer)
	return ok && requirer.RequiresORMTxSession()
}
