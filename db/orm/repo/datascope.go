package repo

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"gochen/contextx"
	"gochen/domain/access"
	"gochen/errors"
)

type dataScopeField struct {
	column string
	index  []int
}

func (f dataScopeField) hasField() bool {
	return len(f.index) > 0
}

type accessColumns struct {
	managedScope string
	ownerID      string
	version      string
}

type dataScopeSchema struct {
	managedScope dataScopeField
	ownerID      dataScopeField
	version      dataScopeField
}

func (r *Repo[T, ID]) applyDataScope(quer *queryBuilder) (*queryBuilder, error) {
	schema := r.accessSchema()
	if quer == nil {
		return nil, nil
	}
	if tenantID := tenantFilterValue(quer.ctx, schema); tenantID != "" {
		quer = quer.Where(schema.ownerID.column+" = ?", tenantID)
	}
	if schema.managedScope.column == "" {
		return quer, nil
	}
	scope, err := r.resolveDataScope(quer.ctx)
	if err != nil {
		return nil, err
	}
	if scope.Mode == access.ScopeModeGlobal {
		return quer, nil
	}
	visibleScopeIDs := normalizeVisibleScopeIDs(scope)
	if len(visibleScopeIDs) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "visible scope IDs are required under managed scope mode")
	}
	if len(visibleScopeIDs) == 1 {
		return quer.Where(schema.managedScope.column+" = ?", visibleScopeIDs[0]), nil
	}
	return quer.Where(schema.managedScope.column+" IN ?", int64SliceToAny(visibleScopeIDs)), nil
}

func (r *Repo[T, ID]) applyDataScopeToEntity(ctx context.Context, entity T) error {
	schema := r.accessSchema()
	value := reflect.ValueOf(entity)
	if !value.IsValid() {
		return nil
	}
	if tenantID := tenantFilterValue(ctx, schema); tenantID != "" {
		if err := assignStringConstraintField(value, schema.ownerID, tenantID, "tenant_id"); err != nil {
			return err
		}
	}
	if schema.managedScope.column == "" {
		return nil
	}
	scope, err := r.resolveDataScope(ctx)
	if err != nil {
		return err
	}
	if scope.Mode == access.ScopeModeGlobal {
		return nil
	}
	visibleScopeIDs := normalizeVisibleScopeIDs(scope)
	if len(visibleScopeIDs) == 0 {
		return errors.NewCode(errors.InvalidInput, "visible scope IDs are required under managed scope mode")
	}
	if len(visibleScopeIDs) == 1 {
		return assignInt64ConstraintField(value, schema.managedScope, visibleScopeIDs[0], "managed_scope_id")
	}
	managedScopeID, err := readRequiredInt64ConstraintField(value, schema.managedScope, "managed_scope_id")
	if err != nil {
		return err
	}
	if !containsInt64(visibleScopeIDs, managedScopeID) {
		return errors.NewCode(errors.Forbidden, "managed scope is outside data scope").
			WithContext("managed_scope_id", managedScopeID)
	}
	return nil
}

func (r *Repo[T, ID]) resolveDataScope(ctx context.Context) (access.DataScope, error) {
	resolver := r.dataScope
	if resolver == nil {
		resolver = access.ContextDataScopeResolver{}
	}
	scope, err := access.ResolveDataScope(ctx, resolver)
	if err != nil {
		if errors.Is(err, errors.Unauthorized) {
			return access.DataScope{}, errors.NewCode(errors.InvalidInput, "data scope is required")
		}
		return access.DataScope{}, err
	}
	return scope, nil
}

func (r *Repo[T, ID]) accessSchema() dataScopeSchema {
	return inferDataScopeSchema[T](r.accessColumns)
}

func (r *Repo[T, ID]) versionColumn() string {
	schema := r.accessSchema()
	if schema.version.column != "" {
		return schema.version.column
	}
	return "version"
}
func tenantFilterValue(ctx context.Context, schema dataScopeSchema) string {
	if schema.ownerID.column != "tenant_id" {
		return ""
	}
	return strings.TrimSpace(contextx.TenantID(ctx))
}
func normalizeVisibleScopeIDs(scope access.DataScope) []int64 {
	ids := append([]int64(nil), scope.VisibleScopeIDs...)
	if len(ids) == 0 && scope.ActiveScopeID > 0 {
		ids = append(ids, scope.ActiveScopeID)
	}
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, value := range ids {
		value = access.NormalizePositiveID(value)
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsInt64(values []int64, target int64) bool {
	target = access.NormalizePositiveID(target)
	for _, value := range values {
		if access.NormalizePositiveID(value) == target {
			return true
		}
	}
	return false
}

func int64SliceToAny(values []int64) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
