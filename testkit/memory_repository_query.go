package testkit

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gochen/app/query"
	"gochen/domain/spec"
	"gochen/errors"
)

// Query 在内存中执行统一查询请求（实现 app/query.IQueryableRepository）。
//
// 语义与 SQL 仓储对齐：先按规约、软删可见性与隔离键过滤，再应用 Filters/
// Advanced/Sorts，最后分页。Fields 只做字段名校验，内存实现返回完整实体
// （SQL 侧的投影会清空未选字段，内存替身保留原值，便于断言）。
func (r *MemoryRepository[T, ID]) Query(ctx context.Context, opts query.QueryOptions, s spec.Spec) ([]T, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	typ := memoryQueryEntityType[T]()
	if err := validateMemoryQueryOptions(typ, opts); err != nil {
		return nil, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return nil, err
	}
	var result []T
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		matched := make([]T, 0, len(state.order))
		for _, id := range state.order {
			entity := state.entities[id]
			if isolated && !r.visibleInIsolation(entity, isolationID) {
				continue
			}
			visible, matchErr := r.visibleEntity(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if !visible {
				continue
			}
			ok, matchErr := memoryEntityMatchesQuery(entity, opts)
			if matchErr != nil {
				return matchErr
			}
			if ok {
				matched = append(matched, cloneEntityState(entity))
			}
		}
		if sortErr := memorySortEntities(matched, opts.Sorts); sortErr != nil {
			return sortErr
		}
		start, end := pageBounds(len(matched), opts.Offset, opts.Limit)
		result = append([]T(nil), matched[start:end]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []T{}
	}
	return result, nil
}

// QueryOne 返回满足查询条件与排序的第一条记录；无记录返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) QueryOne(ctx context.Context, opts query.QueryOptions, s spec.Spec) (T, error) {
	var zero T
	if err := validateContext(ctx); err != nil {
		return zero, err
	}
	typ := memoryQueryEntityType[T]()
	if err := validateMemoryQueryOptions(typ, opts); err != nil {
		return zero, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return zero, err
	}
	var result T
	found := false
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		matched := make([]T, 0, 1)
		for _, id := range state.order {
			entity := state.entities[id]
			if isolated && !r.visibleInIsolation(entity, isolationID) {
				continue
			}
			visible, matchErr := r.visibleEntity(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if !visible {
				continue
			}
			ok, matchErr := memoryEntityMatchesQuery(entity, opts)
			if matchErr != nil {
				return matchErr
			}
			if ok {
				matched = append(matched, cloneEntityState(entity))
				if len(opts.Sorts) == 0 {
					break
				}
			}
		}
		if len(matched) == 0 {
			return nil
		}
		if sortErr := memorySortEntities(matched, opts.Sorts); sortErr != nil {
			return sortErr
		}
		result = matched[0]
		found = true
		return nil
	})
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, errors.NewCode(errors.NotFound, "record not found")
	}
	return result, nil
}

// QueryCount 统计满足过滤条件的记录数；忽略分页、排序与投影。
func (r *MemoryRepository[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions, s spec.Spec) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	typ := memoryQueryEntityType[T]()
	if err := validateMemoryQueryOptions(typ, opts); err != nil {
		return 0, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		for _, id := range state.order {
			entity := state.entities[id]
			if isolated && !r.visibleInIsolation(entity, isolationID) {
				continue
			}
			visible, matchErr := r.visibleEntity(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if !visible {
				continue
			}
			ok, matchErr := memoryEntityMatchesQuery(entity, opts)
			if matchErr != nil {
				return matchErr
			}
			if ok {
				count++
			}
		}
		return nil
	})
	return count, err
}

func memoryQueryEntityType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// validateMemoryQueryOptions 在读取前校验字段、排序方向与操作符，
// 避免空结果集时静默放过非法请求。
func validateMemoryQueryOptions(typ reflect.Type, opts query.QueryOptions) error {
	for _, field := range opts.Fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if _, ok := memoryResolveQueryColumn(typ, name); !ok {
			return errors.NewCode(errors.InvalidInput, "invalid select field").
				WithContext("field", name)
		}
	}
	for _, item := range opts.Sorts {
		field := strings.TrimSpace(item.Field)
		if field == "" {
			return errors.NewCode(errors.InvalidInput, "sort field is required")
		}
		if !item.Direction.IsValid() {
			return errors.NewCode(errors.InvalidInput, "invalid sort direction").
				WithContext("field", field).
				WithContext("direction", string(item.Direction))
		}
		if _, ok := memoryResolveQueryColumn(typ, field); !ok {
			return errors.NewCode(errors.InvalidInput, "invalid order by field").
				WithContext("field", field)
		}
	}
	for rawField, exprs := range opts.Filters {
		field := strings.TrimSpace(rawField)
		if field == "" {
			return errors.NewCode(errors.InvalidInput, "filter field is required")
		}
		if _, ok := memoryResolveQueryColumn(typ, field); !ok {
			return errors.NewCode(errors.InvalidInput, "invalid filter field").
				WithContext("field", field)
		}
		for _, expr := range exprs {
			if !expr.Op.IsValid() {
				return errors.NewCode(errors.InvalidInput, "invalid filter operator").
					WithContext("field", field).
					WithContext("operator", string(expr.Op))
			}
		}
	}
	for _, condition := range opts.Advanced.Or {
		for rawField := range condition {
			field := strings.TrimSpace(rawField)
			if field == "" {
				return errors.NewCode(errors.InvalidInput, "advanced filter field is required")
			}
			if _, ok := memoryResolveQueryColumn(typ, field); !ok {
				return errors.NewCode(errors.InvalidInput, "invalid advanced filter field").
					WithContext("field", field)
			}
		}
	}
	if opts.Advanced.DateRange != nil {
		if _, ok := memoryResolveQueryColumn(typ, "created_at"); !ok {
			return errors.NewCode(errors.InvalidInput, "date range uses hard-coded field created_at, but it is not available").
				WithContext("field", "created_at")
		}
	}
	return nil
}

// memoryResolveQueryColumn 解析查询字段：优先列名，其次 Go 字段名（与
// runtime 侧 resolveAllowedColumn 的 Column/Name 双匹配口径一致）。
func memoryResolveQueryColumn(typ reflect.Type, field string) (string, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return "", false
	}
	if _, ok := memoryFieldByColumn(typ, field); ok {
		return field, true
	}
	if column, ok := memoryFindColumnByGoName(typ, field); ok {
		return column, true
	}
	return "", false
}

// memoryFindColumnByGoName 按 Go 字段名递归查找列名。
func memoryFindColumnByGoName(typ reflect.Type, target string) (string, bool) {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return "", false
	}
	var walk func(t reflect.Type, visiting map[reflect.Type]int) (string, bool)
	walk = func(t reflect.Type, visiting map[reflect.Type]int) (string, bool) {
		if visiting[t] > 0 {
			return "", false
		}
		visiting[t]++
		defer func() { visiting[t]-- }()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" && !field.Anonymous {
				continue
			}
			if field.Name == target {
				return memoryColumnName(field), true
			}
			fieldType := field.Type
			for fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() != reflect.Struct || fieldType.PkgPath() == "time" {
				continue
			}
			if column, ok := walk(fieldType, visiting); ok {
				return column, true
			}
		}
		return "", false
	}
	return walk(typ, map[reflect.Type]int{})
}

func memoryEntityMatchesQuery(entity any, opts query.QueryOptions) (bool, error) {
	for field, exprs := range opts.Filters {
		for _, expr := range exprs {
			ok, err := memoryMatchFilter(entity, field, expr)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
	}
	if opts.Advanced.IsZero() {
		return true, nil
	}
	return memoryMatchAdvanced(entity, opts.Advanced)
}

func memoryMatchFilter(entity any, field string, expr query.QueryExpr) (bool, error) {
	column, ok := memoryResolveQueryColumn(reflect.TypeOf(entity), field)
	if !ok {
		return false, errors.NewCode(errors.InvalidInput, "invalid filter field").
			WithContext("field", field)
	}
	value, ok := memoryQueryFieldValue(entity, column)
	if !ok {
		return false, errors.NewCode(errors.InvalidInput, "field cannot be read").
			WithContext("field", field)
	}
	switch expr.Op {
	case query.FilterOpIsNull:
		return memoryIsNullValue(value), nil
	case query.FilterOpNotNull:
		return !memoryIsNullValue(value), nil
	case query.FilterOpLike:
		text, ok := memoryStringValue(value)
		if !ok {
			return false, errors.NewCode(errors.InvalidInput, "like filter requires a string field").
				WithContext("field", field)
		}
		return strings.Contains(text, expr.Value.String), nil
	case query.FilterOpIn, query.FilterOpNotIn:
		found := false
		for _, candidate := range expr.Values {
			equal, err := memoryFieldEqual(value, candidate)
			if err != nil {
				return false, err
			}
			if equal {
				found = true
				break
			}
		}
		if expr.Op == query.FilterOpIn {
			return found, nil
		}
		return !found, nil
	case query.FilterOpEq, query.FilterOpNe:
		equal, err := memoryFieldEqual(value, expr.Value)
		if err != nil {
			return false, err
		}
		if expr.Op == query.FilterOpEq {
			return equal, nil
		}
		return !equal, nil
	case query.FilterOpGt, query.FilterOpGte, query.FilterOpLt, query.FilterOpLte:
		cmp, err := memoryFieldCompare(value, expr.Value)
		if err != nil {
			return false, err
		}
		switch expr.Op {
		case query.FilterOpGt:
			return cmp > 0, nil
		case query.FilterOpGte:
			return cmp >= 0, nil
		case query.FilterOpLt:
			return cmp < 0, nil
		default:
			return cmp <= 0, nil
		}
	default:
		return false, errors.NewCode(errors.InvalidInput, "invalid filter operator").
			WithContext("field", field).
			WithContext("operator", string(expr.Op))
	}
}

func memoryMatchAdvanced(entity any, advanced query.AdvancedFilters) (bool, error) {
	if len(advanced.Or) > 0 {
		typ := reflect.TypeOf(entity)
		matched := false
		for _, condition := range advanced.Or {
			if len(condition) == 0 {
				continue
			}
			groupMatched := true
			for rawField, expected := range condition {
				field := strings.TrimSpace(rawField)
				column, ok := memoryResolveQueryColumn(typ, field)
				if !ok {
					return false, errors.NewCode(errors.InvalidInput, "invalid advanced filter field").
						WithContext("field", field)
				}
				value, ok := memoryQueryFieldValue(entity, column)
				if !ok {
					return false, errors.NewCode(errors.InvalidInput, "field cannot be read").
						WithContext("field", field)
				}
				actual, ok := memoryNormalizedString(value)
				if !ok || actual != expected {
					groupMatched = false
					break
				}
			}
			if groupMatched {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if advanced.DateRange != nil {
		typ := reflect.TypeOf(entity)
		column, ok := memoryResolveQueryColumn(typ, "created_at")
		if !ok {
			return false, errors.NewCode(errors.InvalidInput, "date range uses hard-coded field created_at, but it is not available")
		}
		value, ok := memoryQueryFieldValue(entity, column)
		if !ok {
			return false, errors.NewCode(errors.InvalidInput, "field cannot be read").
				WithContext("field", "created_at")
		}
		actual, ok := memoryTimeValue(value)
		if !ok {
			return false, errors.NewCode(errors.InvalidInput, "date range requires a time field").
				WithContext("field", "created_at")
		}
		if advanced.DateRange.Start != "" {
			start, err := memoryParseTime(advanced.DateRange.Start)
			if err != nil {
				return false, err
			}
			if actual.Before(start) {
				return false, nil
			}
		}
		if advanced.DateRange.End != "" {
			end, err := memoryParseTime(advanced.DateRange.End)
			if err != nil {
				return false, err
			}
			if actual.After(end) {
				return false, nil
			}
		}
	}
	return true, nil
}

func memorySortEntities[T any](entities []T, sorts []query.Sort) error {
	if len(sorts) == 0 {
		return nil
	}
	typ := memoryQueryEntityType[T]()
	columns := make([]string, len(sorts))
	for i, item := range sorts {
		column, ok := memoryResolveQueryColumn(typ, item.Field)
		if !ok {
			return errors.NewCode(errors.InvalidInput, "invalid order by field").
				WithContext("field", item.Field)
		}
		columns[i] = column
	}
	var sortErr error
	sort.SliceStable(entities, func(i, j int) bool {
		for index, item := range sorts {
			cmp, err := memoryCompareEntityField(entities[i], entities[j], columns[index])
			if err != nil {
				if sortErr == nil {
					sortErr = err
				}
				return false
			}
			if cmp == 0 {
				continue
			}
			if item.Direction == query.DESC {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	return sortErr
}

func memoryCompareEntityField(left, right any, column string) (int, error) {
	leftValue, ok := memoryQueryFieldValue(left, column)
	if !ok {
		return 0, errors.NewCode(errors.InvalidInput, "sort field cannot be read").
			WithContext("field", column)
	}
	rightValue, ok := memoryQueryFieldValue(right, column)
	if !ok {
		return 0, errors.NewCode(errors.InvalidInput, "sort field cannot be read").
			WithContext("field", column)
	}
	return memoryCompareFieldValues(leftValue, rightValue, column)
}

// memoryQueryFieldValue 按列名读取实体字段值（穿透指针与嵌入结构体）。
func memoryQueryFieldValue(entity any, column string) (reflect.Value, bool) {
	value := reflect.ValueOf(entity)
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return memoryFieldValueByIndex(value, column)
}

func memoryIsNullValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}

func memoryDeref(value reflect.Value) (reflect.Value, bool) {
	for value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, true
}

func memoryStringValue(value reflect.Value) (string, bool) {
	deref, ok := memoryDeref(value)
	if !ok {
		return "", false
	}
	if deref.Kind() != reflect.String {
		return "", false
	}
	return deref.String(), true
}

func memoryTimeValue(value reflect.Value) (time.Time, bool) {
	deref, ok := memoryDeref(value)
	if !ok || deref.Type() != reflect.TypeOf(time.Time{}) {
		return time.Time{}, false
	}
	return deref.Interface().(time.Time), true
}

// memoryNormalizedString 把字段值渲染为可比较的规范字符串；
// 用于 eq/in 与高级过滤的宽松类型匹配（对齐 SQL 的隐式转换）。
func memoryNormalizedString(value reflect.Value) (string, bool) {
	deref, ok := memoryDeref(value)
	if !ok {
		return "", false
	}
	if deref.Type() == reflect.TypeOf(time.Time{}) {
		return deref.Interface().(time.Time).Format(time.RFC3339Nano), true
	}
	switch deref.Kind() {
	case reflect.String:
		return deref.String(), true
	case reflect.Bool:
		return strconv.FormatBool(deref.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(deref.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(deref.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(deref.Float(), 'g', -1, 64), true
	default:
		return "", false
	}
}

func memoryExpectedNormalized(value query.QueryValue) string {
	if value.Normalized != "" {
		return value.Normalized
	}
	switch value.Type {
	case query.FieldTypeInt:
		return strconv.FormatInt(value.Int, 10)
	case query.FieldTypeFloat:
		return strconv.FormatFloat(value.Float, 'g', -1, 64)
	case query.FieldTypeBool:
		return strconv.FormatBool(value.Bool)
	case query.FieldTypeTime:
		return value.Time.Format(time.RFC3339Nano)
	default:
		return value.String
	}
}

func memoryFieldEqual(value reflect.Value, expected query.QueryValue) (bool, error) {
	actual, ok := memoryNormalizedString(value)
	if !ok {
		return false, errors.NewCode(errors.InvalidInput, "filter value type is not comparable").
			WithContext("type", value.Type().String())
	}
	return actual == memoryExpectedNormalized(expected), nil
}

func memoryFieldCompare(value reflect.Value, expected query.QueryValue) (int, error) {
	actual, ok := memoryDeref(value)
	if !ok {
		return 0, errors.NewCode(errors.InvalidInput, "range filter cannot compare a null value")
	}
	if actual.Type() == reflect.TypeOf(time.Time{}) {
		actualTime := actual.Interface().(time.Time)
		expectedTime, err := memoryParseTime(memoryExpectedNormalized(expected))
		if err != nil {
			return 0, err
		}
		return actualTime.Compare(expectedTime), nil
	}
	switch actual.Kind() {
	case reflect.String:
		return strings.Compare(actual.String(), expected.String), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return compareInt64(actual.Int(), memoryExpectedInt(expected)), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		expectedInt := memoryExpectedInt(expected)
		if expectedInt < 0 {
			return 1, nil
		}
		return compareUint64(actual.Uint(), uint64(expectedInt)), nil
	case reflect.Float32, reflect.Float64:
		return compareFloat64(actual.Float(), memoryExpectedFloat(expected)), nil
	default:
		return 0, errors.NewCode(errors.InvalidInput, "range filter requires an ordered field").
			WithContext("type", actual.Type().String())
	}
}

func memoryCompareFieldValues(left, right reflect.Value, column string) (int, error) {
	if memoryIsNullValue(left) || memoryIsNullValue(right) {
		switch {
		case memoryIsNullValue(left) && memoryIsNullValue(right):
			return 0, nil
		case memoryIsNullValue(left):
			return -1, nil
		default:
			return 1, nil
		}
	}
	leftValue, ok := memoryDeref(left)
	if !ok {
		return 0, errors.NewCode(errors.InvalidInput, "sort field cannot be read").
			WithContext("field", column)
	}
	rightValue, ok := memoryDeref(right)
	if !ok {
		return 0, errors.NewCode(errors.InvalidInput, "sort field cannot be read").
			WithContext("field", column)
	}
	if leftValue.Type() == reflect.TypeOf(time.Time{}) && rightValue.Type() == reflect.TypeOf(time.Time{}) {
		return leftValue.Interface().(time.Time).Compare(rightValue.Interface().(time.Time)), nil
	}
	switch leftValue.Kind() {
	case reflect.String:
		return strings.Compare(leftValue.String(), rightValue.String()), nil
	case reflect.Bool:
		return compareBool(leftValue.Bool(), rightValue.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return compareInt64(leftValue.Int(), rightValue.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return compareUint64(leftValue.Uint(), rightValue.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return compareFloat64(leftValue.Float(), rightValue.Float()), nil
	default:
		return 0, errors.NewCode(errors.InvalidInput, "sort field type is not orderable").
			WithContext("field", column).
			WithContext("type", leftValue.Type().String())
	}
}

func memoryExpectedInt(value query.QueryValue) int64 {
	if value.Type == query.FieldTypeInt {
		return value.Int
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(memoryExpectedNormalized(value)), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func memoryExpectedFloat(value query.QueryValue) float64 {
	if value.Type == query.FieldTypeFloat {
		return value.Float
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(memoryExpectedNormalized(value)), 64)
	if err != nil {
		return 0
	}
	return parsed
}

func memoryParseTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.NewCode(errors.InvalidInput, "time value is required")
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.NewCode(errors.InvalidInput, "invalid time value").
		WithContext("value", raw)
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareUint64(left, right uint64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareFloat64(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareBool(left, right bool) int {
	switch {
	case left == right:
		return 0
	case !left:
		return -1
	default:
		return 1
	}
}
