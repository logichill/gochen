package repo

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gochen/domain/access"
	"gochen/errors"
)

// CreateWithConstraint 在显式写入约束下创建实体。
func (r *Repo[T, ID]) CreateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	if err := r.prepareCreate(ctx, entity); err != nil {
		return err
	}
	resource, err := r.requireConstraintResource(constraint, entity.GetID(), true)
	if err != nil {
		return err
	}
	if err := r.applyConstraintToEntity(entity, resource); err != nil {
		return err
	}
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	if err := quer.Create(entity); err != nil {
		return errors.Wrap(err, errors.Database, "failed to save record")
	}
	access.RecordWriteConstraintAudit(ctx, "create", constraint, resource)
	return nil
}

// UpdateWithConstraint 在显式写入约束下更新实体。
func (r *Repo[T, ID]) UpdateWithConstraint(ctx context.Context, entity T, constraint access.WriteConstraint) error {
	resource, expectedVersion, err := r.requireVersionedConstraint(constraint, entity.GetID(), false)
	if err != nil {
		return err
	}
	if entity.GetVersion() != 0 && entity.GetVersion() != expectedVersion {
		return errors.NewCode(errors.Forbidden, "write constraint revision does not match entity version").
			WithContext("resource_id", formatResourceID(entity.GetID())).
			WithContext("expected_version", expectedVersion).
			WithContext("entity_version", entity.GetVersion())
	}
	if err := r.prepareUpdate(ctx, entity); err != nil {
		return err
	}
	if err := r.applyConstraintToEntity(entity, resource); err != nil {
		return err
	}
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	quer = quer.Where("id = ?", entity.GetID())
	quer, err = r.applyConstraintFilters(quer, resource, expectedVersion)
	if err != nil {
		return err
	}
	result, err := quer.SaveWithResult(entity)
	if err != nil {
		if errors.Is(err, errors.Unsupported) {
			return errors.NewCode(errors.Unsupported, "constrained update requires an orm model with result support")
		}
		return errors.Wrap(err, errors.Database, "failed to update record")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, errors.Database, "failed to read constrained update result")
	}
	if affected == 0 {
		return r.classifyConstraintWriteMiss(ctx, entity.GetID(), expectedVersion)
	}
	access.RecordWriteConstraintAudit(ctx, "update", constraint, resource)
	return nil
}

// DeleteWithConstraint 在显式写入约束下删除实体。
func (r *Repo[T, ID]) DeleteWithConstraint(ctx context.Context, id ID, constraint access.WriteConstraint) error {
	resource, expectedVersion, err := r.requireVersionedConstraint(constraint, id, false)
	if err != nil {
		return err
	}
	quer, err := r.query(ctx)
	if err != nil {
		return err
	}
	quer = quer.Where("id = ?", id)
	quer, err = r.applyConstraintFilters(quer, resource, expectedVersion)
	if err != nil {
		return err
	}
	if r.softDelete {
		nowValues, err := r.softDeleteValues(ctx)
		if err != nil {
			return err
		}
		result, err := quer.Where(r.softDeleteCols.DeletedAt + " IS NULL").UpdateValuesWithResult(nowValues)
		if err != nil {
			if errors.Is(err, errors.Unsupported) {
				return errors.NewCode(errors.Unsupported, "constrained delete requires an orm model with result support")
			}
			return errors.Wrap(err, errors.Database, "failed to delete record")
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return errors.Wrap(err, errors.Database, "failed to read constrained delete result")
		}
		if affected == 0 {
			return r.classifyConstraintWriteMiss(ctx, id, expectedVersion)
		}
		access.RecordWriteConstraintAudit(ctx, "delete", constraint, resource)
		return nil
	}
	result, err := quer.DeleteWithResult()
	if err != nil {
		if errors.Is(err, errors.Unsupported) {
			return errors.NewCode(errors.Unsupported, "constrained delete requires an orm model with result support")
		}
		return errors.Wrap(err, errors.Database, "failed to delete record")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, errors.Database, "failed to read constrained delete result")
	}
	if affected == 0 {
		return r.classifyConstraintWriteMiss(ctx, id, expectedVersion)
	}
	access.RecordWriteConstraintAudit(ctx, "delete", constraint, resource)
	return nil
}

func (r *Repo[T, ID]) requireConstraintResource(constraint access.WriteConstraint, id ID, allowGenericID bool) (access.ResourceConstraint, error) {
	resourceID := formatResourceID(id)
	if !allowGenericID && resourceID == "" {
		return access.ResourceConstraint{}, errors.NewCode(errors.InvalidInput, "resource ID is required for constrained write")
	}
	resource, err := constraint.RequireResource(r.writeResourceKind(), resourceID)
	if err != nil {
		return access.ResourceConstraint{}, err
	}
	if !allowGenericID && resourceID != "" && resource.ResourceID == "" {
		return access.ResourceConstraint{}, errors.NewCode(errors.Forbidden, "write constraint must bind to a concrete resource ID").
			WithContext("resource_kind", r.writeResourceKind()).
			WithContext("resource_id", resourceID)
	}
	return resource, nil
}

func (r *Repo[T, ID]) requireVersionedConstraint(constraint access.WriteConstraint, id ID, allowGenericID bool) (access.ResourceConstraint, uint64, error) {
	resource, err := r.requireConstraintResource(constraint, id, allowGenericID)
	if err != nil {
		return access.ResourceConstraint{}, 0, err
	}
	expectedVersion, err := parseConstraintRevision(resource.Revision)
	if err != nil {
		return access.ResourceConstraint{}, 0, err
	}
	return resource, expectedVersion, nil
}

func (r *Repo[T, ID]) applyConstraintFilters(quer *queryBuilder, constraint access.ResourceConstraint, expectedVersion uint64) (*queryBuilder, error) {
	schema := r.accessSchema()
	if constraint.ManagedScopeID != 0 {
		if schema.managedScope.column == "" {
			return nil, errors.NewCode(errors.Forbidden, "write constraint managed_scope_id cannot be enforced for this model")
		}
		quer = quer.Where(schema.managedScope.column+" = ?", constraint.ManagedScopeID)
	}
	quer = quer.Where(r.versionColumn()+" = ?", expectedVersion)
	return quer, nil
}

func (r *Repo[T, ID]) applyConstraintToEntity(entity T, constraint access.ResourceConstraint) error {
	schema := r.accessSchema()
	if !schema.hasConstraints() {
		return nil
	}
	if constraint.ManagedScopeID == 0 {
		return nil
	}
	if schema.managedScope.column == "" {
		return errors.NewCode(errors.Forbidden, "write constraint managed_scope_id cannot be enforced for this model")
	}
	return alignConstraintManagedScopeField(entity, schema.managedScope, constraint.ManagedScopeID)
}

func alignConstraintManagedScopeField[T any](entity T, field dataScopeField, target int64) error {
	if !field.hasField() {
		return errors.NewCode(errors.Forbidden, "write constraint cannot align missing entity field").
			WithContext("field", "managed_scope_id").
			WithContext("column", field.column)
	}
	current, ok := readInt64Field(reflectValue(entity), field.index)
	if ok && current != 0 && current != access.NormalizePositiveID(target) {
		return errors.NewCode(errors.Forbidden, "write constraint does not authorize the target managed_scope_id").
			WithContext("managed_scope_id", current).
			WithContext("authorized_managed_scope_id", access.NormalizePositiveID(target))
	}
	if !setInt64Field(reflectValue(entity), field.index, target) {
		return errors.NewCode(errors.Forbidden, "write constraint cannot write the target managed_scope_id").
			WithContext("column", field.column)
	}
	return nil
}

func reflectValue(value any) reflect.Value { return reflect.ValueOf(value) }

func (r *Repo[T, ID]) classifyConstraintWriteMiss(ctx context.Context, id ID, expectedVersion uint64) error {
	entity, err := r.get(ctx, id, false)
	if err == nil {
		if entity.GetVersion() != expectedVersion {
			return errors.NewCode(errors.Conflict, "record revision mismatch").
				WithContext("id", id).
				WithContext("expected_version", expectedVersion).
				WithContext("actual_version", entity.GetVersion())
		}
		return errors.NewCode(errors.Conflict, "constrained write did not affect any record").
			WithContext("id", id).
			WithContext("expected_version", expectedVersion)
	}
	if !errors.Is(err, errors.NotFound) {
		return err
	}
	exists, err := r.existsIgnoringDataScope(ctx, id)
	if err != nil {
		return err
	}
	notFound := errors.NewCode(errors.NotFound, "record not found").
		WithContext("id", id).
		WithContext("expected_version", expectedVersion)
	if !exists {
		return notFound
	}
	return notFound
}

func (r *Repo[T, ID]) existsIgnoringDataScope(ctx context.Context, id ID) (bool, error) {
	model, err := r.ModelFor(ctx)
	if err != nil {
		return false, err
	}
	count, err := newQueryBuilder(model, ctx).Where("id = ?", id).Count()
	if err != nil {
		return false, errors.Wrap(err, errors.Database, "failed to check constrained record existence")
	}
	return count > 0, nil
}

func (r *Repo[T, ID]) softDeleteValues(ctx context.Context) (map[string]any, error) {
	now := time.Now()
	by := r.actorFromContext(ctx)
	values := map[string]any{r.softDeleteCols.DeletedAt: now}
	if r.softDeleteCols.DeletedBy != "" {
		values[r.softDeleteCols.DeletedBy] = by
	}
	if r.auditFields {
		values["updated_at"] = now
		values["updated_by"] = by
	}
	return values, nil
}

func (r *Repo[T, ID]) writeResourceKind() string {
	if kind := strings.TrimSpace(r.resourceKind); kind != "" {
		return kind
	}
	return strings.TrimSpace(r.meta.Table)
}

func parseConstraintRevision(revision string) (uint64, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return 0, errors.NewCode(errors.InvalidInput, "write constraint revision is required")
	}
	version, err := strconv.ParseUint(revision, 10, 64)
	if err != nil {
		return 0, errors.NewCode(errors.InvalidInput, "write constraint revision must be an unsigned integer").
			WithContext("revision", revision)
	}
	return version, nil
}

func formatResourceID[ID comparable](id ID) string {
	var zero ID
	if id == zero {
		return ""
	}
	return fmt.Sprint(id)
}
