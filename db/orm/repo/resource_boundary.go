package repo

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gochen/domain/access"
	"gochen/errors"
)

// ResolveResourceByID 在不套用 DataScope 的前提下，按主键解析目标资源的授权边界。
func (r *Repo[T, ID]) ResolveResourceByID(ctx context.Context, id ID) (access.ResourceBoundary, error) {
	model, err := r.ModelFor(ctx)
	if err != nil {
		return access.ResourceBoundary{}, err
	}

	entity, err := instantiateEntity[T]()
	if err != nil {
		return access.ResourceBoundary{}, err
	}
	query := newQueryBuilder(model, ctx).Where("id = ?", id).Select(r.resourceBoundaryColumns()...)
	if r.softDelete {
		query = query.Where(r.softDeleteCols.DeletedAt + " IS NULL")
	}
	if err := query.First(entity); err != nil {
		if errors.Is(err, errors.NotFound) {
			return access.ResourceBoundary{}, errors.NewCode(errors.NotFound, "record not found")
		}
		return access.ResourceBoundary{}, errors.Wrap(err, errors.Database, "failed to resolve resource boundary")
	}

	return r.resourceFromEntity(entity), nil
}

func (r *Repo[T, ID]) resourceBoundaryColumns() []string {
	columns := []string{"id"}
	schema := r.accessSchema()
	appendIfMissing := func(column string) {
		column = strings.TrimSpace(column)
		if column == "" {
			return
		}
		for _, existing := range columns {
			if existing == column {
				return
			}
		}
		columns = append(columns, column)
	}

	appendIfMissing(schema.managedScope.column)
	appendIfMissing(schema.ownerID.column)
	appendIfMissing(schema.version.column)
	return columns
}

func (r *Repo[T, ID]) resourceFromEntity(entity T) access.ResourceBoundary {
	resource := access.ResourceBoundary{Kind: strings.TrimSpace(r.writeResourceKind())}
	resource.ID = fmt.Sprint(entity.GetID())

	schema := r.accessSchema()
	value := reflect.ValueOf(entity)
	if schema.managedScope.column != "" {
		resource.ManagedScopeID, _ = readInt64Field(value, schema.managedScope.index)
	}
	if schema.ownerID.column != "" {
		resource.OwnerID, _ = readStringField(value, schema.ownerID.index)
	}
	if schema.version.column != "" {
		resource.Revision = readRevisionField(value, schema.version.index)
	}
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.ManagedScopeID = access.NormalizePositiveID(resource.ManagedScopeID)
	resource.OwnerID = strings.TrimSpace(resource.OwnerID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	return resource
}

func readRevisionField(value reflect.Value, index []int) string {
	field, ok := resolveField(value, index)
	if !ok {
		return ""
	}
	for field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return ""
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.String:
		return strings.TrimSpace(field.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(field.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(field.Uint(), 10)
	default:
		return ""
	}
}

func instantiateEntity[T any]() (T, error) {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ == nil {
		return zero, errors.NewCode(errors.InvalidInput, "entity type is not initialized")
	}
	var instance any
	if typ.Kind() == reflect.Ptr {
		instance = reflect.New(typ.Elem()).Interface()
	} else {
		instance = reflect.New(typ).Elem().Interface()
	}
	entity, ok := instance.(T)
	if !ok {
		return zero, errors.NewCode(errors.Internal, "failed to instantiate entity").
			WithContext("entity_type", fmt.Sprintf("%T", zero))
	}
	return entity, nil
}
