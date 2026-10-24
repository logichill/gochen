package query

import (
	"math"

	"gochen/errors"
)

type queryValueBound struct {
	Value     QueryValue
	Inclusive bool
}

type queryValueRange struct {
	Lower *queryValueBound
	Upper *queryValueBound
}

// fieldSupportsAutoRange 判断字段是否允许「同字段多表达式自动合并为区间」。
//
// 包含可比较的有序类型：数值、时间，以及可按字典序比较的字符串/枚举。
// 必须同时满足声明了至少一个范围操作符（gt/gte/lt/lte）。
// 不包含布尔：布尔只有 eq/ne，不存在区间语义，允许合并只会掩盖非法组合。
func fieldSupportsAutoRange(field QueryField) bool {
	switch field.Type {
	case FieldTypeInt, FieldTypeFloat, FieldTypeTime, FieldTypeString, FieldTypeEnum:
		return field.supportsFilterOp(FilterOpGt) ||
			field.supportsFilterOp(FilterOpGte) ||
			field.supportsFilterOp(FilterOpLt) ||
			field.supportsFilterOp(FilterOpLte)
	default:
		return false
	}
}

func validateFieldExpressions(field QueryField, exprs []QueryExpr) error {
	if len(exprs) <= 1 {
		return nil
	}
	if !fieldSupportsAutoRange(field) {
		return errors.NewCode(errors.InvalidInput, "multiple filter expressions are not supported for this field").
			WithContext("field", field.Name)
	}
	_, _, err := parseQueryValueRange(exprs, true)
	if err != nil {
		return err.WithContext("field", field.Name)
	}
	return nil
}

func parseQueryValueRange(exprs []QueryExpr, strict bool) (queryValueRange, bool, errors.IAppError) {
	if len(exprs) == 0 {
		return queryValueRange{}, false, nil
	}

	var (
		result queryValueRange
		exact  *queryValueBound
		seen   bool
	)

	for _, expr := range exprs {
		if expr.Value.Type == FieldTypeFloat {
			if math.IsNaN(expr.Value.Float) {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range float value cannot be NaN")
			}
			if math.IsInf(expr.Value.Float, 0) {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range float value cannot be infinite")
			}
		}
		switch expr.Op {
		case FilterOpEq:
			if exact != nil || result.Lower != nil || result.Upper != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			exact = &queryValueBound{Value: expr.Value, Inclusive: true}
			seen = true
		case FilterOpGt, FilterOpGte:
			if exact != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			if result.Lower != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has duplicate lower bound")
			}
			result.Lower = &queryValueBound{Value: expr.Value, Inclusive: expr.Op == FilterOpGte}
			seen = true
		case FilterOpLt, FilterOpLte:
			if exact != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			if result.Upper != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has duplicate upper bound")
			}
			result.Upper = &queryValueBound{Value: expr.Value, Inclusive: expr.Op == FilterOpLte}
			seen = true
		default:
			if strict {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range contains unsupported operator").WithContext("operator", string(expr.Op))
			}
		}
	}

	if !seen {
		return queryValueRange{}, false, nil
	}
	if exact != nil {
		result.Lower = exact
		result.Upper = exact
	}
	if err := validateRangeBounds(result); err != nil {
		return queryValueRange{}, false, err
	}
	return result, true, nil
}

func validateRangeBounds(r queryValueRange) errors.IAppError {
	if r.Lower == nil || r.Upper == nil {
		return nil
	}
	cmp, err := compareQueryValues(r.Lower.Value, r.Upper.Value)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "query range compare failed")
	}
	if cmp > 0 {
		return errors.NewCode(errors.InvalidInput, "query range lower bound is greater than upper bound")
	}
	if cmp == 0 && (!r.Lower.Inclusive || !r.Upper.Inclusive) {
		return errors.NewCode(errors.InvalidInput, "query range is empty")
	}
	return nil
}

func compareQueryValues(left, right QueryValue) (int, error) {
	if left.Type == "" || right.Type == "" {
		return 0, missingRangeValueTypeError(left.Type, right.Type)
	}
	typ := left.Type
	if left.Type != right.Type {
		return 0, errors.NewCode(errors.InvalidInput, "query range type mismatch").WithContext("left", string(left.Type)).WithContext("right", string(right.Type))
	}
	switch typ {
	case FieldTypeInt:
		switch {
		case left.Int < right.Int:
			return -1, nil
		case left.Int > right.Int:
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeFloat:
		if math.IsNaN(left.Float) || math.IsNaN(right.Float) {
			return 0, errors.NewCode(errors.InvalidInput, "query range float value cannot be NaN")
		}
		if math.IsInf(left.Float, 0) || math.IsInf(right.Float, 0) {
			return 0, errors.NewCode(errors.InvalidInput, "query range float value cannot be infinite")
		}
		switch {
		case left.Float < right.Float:
			return -1, nil
		case left.Float > right.Float:
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeTime:
		switch {
		case left.Time.Before(right.Time):
			return -1, nil
		case left.Time.After(right.Time):
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeString, FieldTypeEnum:
		lv := left.Normalized
		if lv == "" {
			lv = left.String
		}
		rv := right.Normalized
		if rv == "" {
			rv = right.String
		}
		switch {
		case lv < rv:
			return -1, nil
		case lv > rv:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, errors.NewCode(errors.InvalidInput, "query range type is not comparable").WithContext("type", string(typ))
	}
}
