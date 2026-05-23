package repo

import (
	"context"
	"strconv"

	"gochen/contextx"
	"gochen/domain/access"
)

// CreateAllWithConstraint 在显式写入约束下批量创建实体。
func (r *Repo[T, ID]) CreateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	if len(entities) == 0 {
		return nil
	}
	split, err := constraint.SplitByTargets(r.batchConstraintTargetsForEntities(ctx, entities))
	if err != nil {
		return err
	}
	return r.withConstrainedBatchTx(ctx, func(txCtx context.Context) error {
		for i, entity := range entities {
			scopedTxCtx, err := split[i].ScopedContext(txCtx)
			if err != nil {
				return err
			}
			if err := r.CreateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateAllWithConstraint 在显式写入约束下批量更新实体。
func (r *Repo[T, ID]) UpdateAllWithConstraint(ctx context.Context, entities []T, constraint access.WriteConstraint) error {
	if len(entities) == 0 {
		return nil
	}
	split, err := constraint.SplitByTargets(r.batchConstraintTargetsForEntities(ctx, entities))
	if err != nil {
		return err
	}
	return r.withConstrainedBatchTx(ctx, func(txCtx context.Context) error {
		for i, entity := range entities {
			scopedTxCtx, err := split[i].ScopedContext(txCtx)
			if err != nil {
				return err
			}
			if err := r.UpdateWithConstraint(scopedTxCtx, entity, split[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteAllWithConstraint 在显式写入约束下批量删除实体。
func (r *Repo[T, ID]) DeleteAllWithConstraint(ctx context.Context, ids []ID, constraint access.WriteConstraint) error {
	if len(ids) == 0 {
		return nil
	}
	split, err := constraint.SplitByTargets(r.batchConstraintTargetsForIDs(ctx, ids))
	if err != nil {
		return err
	}
	return r.withConstrainedBatchTx(ctx, func(txCtx context.Context) error {
		for i, id := range ids {
			scopedTxCtx, err := split[i].ScopedContext(txCtx)
			if err != nil {
				return err
			}
			if err := r.DeleteWithConstraint(scopedTxCtx, id, split[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repo[T, ID]) withConstrainedBatchTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return contextx.RunTxLifecycle(ctx, r, fn)
}

func (r *Repo[T, ID]) batchConstraintTargetsForEntities(ctx context.Context, entities []T) []access.ResourceBoundary {
	targets := make([]access.ResourceBoundary, 0, len(entities))
	for _, entity := range entities {
		target := access.ResourceBoundary{
			Kind:     r.writeResourceKind(),
			ID:       formatResourceID(entity.GetID()),
			Revision: formatBatchConstraintVersion(entity.GetVersion()),
		}
		if target.ID == "" {
			target = r.fillBatchConstraintScope(ctx, entity, target)
		}
		targets = append(targets, target)
	}
	return targets
}

func (r *Repo[T, ID]) batchConstraintTargetsForIDs(_ context.Context, ids []ID) []access.ResourceBoundary {
	targets := make([]access.ResourceBoundary, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, access.ResourceBoundary{Kind: r.writeResourceKind(), ID: formatResourceID(id)})
	}
	return targets
}

func (r *Repo[T, ID]) fillBatchConstraintScope(ctx context.Context, entity T, target access.ResourceBoundary) access.ResourceBoundary {
	schema := r.accessSchema()
	value := reflectValue(entity)
	if schema.managedScope.hasField() {
		if managedScopeID, ok := readInt64Field(value, schema.managedScope.index); ok {
			target.ManagedScopeID = access.NormalizePositiveID(managedScopeID)
		}
	}
	scope, ok := access.DataScopeFromContext(ctx)
	if !ok {
		return target
	}
	if target.ManagedScopeID == 0 {
		visibleScopeIDs := normalizeVisibleScopeIDs(scope)
		if len(visibleScopeIDs) == 1 {
			target.ManagedScopeID = visibleScopeIDs[0]
		}
	}
	return target
}

func formatBatchConstraintVersion(version uint64) string {
	if version == 0 {
		return ""
	}
	return strconv.FormatUint(version, 10)
}
