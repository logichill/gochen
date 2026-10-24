package query

import "gochen/errors"

// NormalizeFilters 校验并规范化过滤条件。
func (s *QuerySchema) NormalizeFilters(filters []Filter) ([]Filter, error) {
	if len(filters) == 0 || s == nil {
		return filters, nil
	}
	out := make([]Filter, 0, len(filters))
	for _, filter := range filters {
		field, ok := s.Field(filter.Field)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter field").
				WithContext("field", filter.Field)
		}
		if !field.supportsFilterOp(filter.Op) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter operator").
				WithContext("field", filter.Field).
				WithContext("operator", string(filter.Op))
		}

		normalized := filter
		switch filter.Op {
		case FilterOpIsNull, FilterOpNotNull:
			normalized.Value = ""
			normalized.Values = nil
		case FilterOpIn, FilterOpNotIn:
			values, _, err := field.normalizeValues(filter.Values)
			if err != nil {
				return nil, err
			}
			normalized.Value = ""
			normalized.Values = values
		default:
			value, _, err := field.normalizeValue(filter.Value)
			if err != nil {
				return nil, err
			}
			normalized.Value = value
			normalized.Values = nil
		}
		out = append(out, normalized)
	}
	return out, nil
}

// DecodeFilters 校验过滤条件并返回带类型信息的查询表达式集合。
func (s *QuerySchema) DecodeFilters(filters []Filter) (QueryFilters, error) {
	if len(filters) == 0 || s == nil {
		return nil, nil
	}
	var out QueryFilters
	for _, filter := range filters {
		field, ok := s.Field(filter.Field)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter field").
				WithContext("field", filter.Field)
		}
		if !field.supportsFilterOp(filter.Op) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter operator").
				WithContext("field", filter.Field).
				WithContext("operator", string(filter.Op))
		}

		decoded := QueryExpr{Op: filter.Op}
		switch filter.Op {
		case FilterOpIsNull, FilterOpNotNull:
			out = out.Append(field.Name, decoded)
			continue
		case FilterOpIn, FilterOpNotIn:
			_, values, err := field.normalizeValues(filter.Values)
			if err != nil {
				return nil, err
			}
			decoded.Values = values
		default:
			_, value, err := field.normalizeValue(filter.Value)
			if err != nil {
				return nil, err
			}
			decoded.Value = value
		}
		out = out.Append(field.Name, decoded)
	}
	if out.IsZero() {
		return nil, nil
	}
	for fieldName, exprs := range out {
		field, ok := s.Field(fieldName)
		if !ok {
			continue
		}
		if err := validateFieldExpressions(field, exprs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ValidateSorts 校验排序字段能力。
func (s *QuerySchema) ValidateSorts(sorts []Sort) error {
	if len(sorts) == 0 || s == nil {
		return nil
	}
	for _, sort := range sorts {
		field, ok := s.Field(sort.Field)
		if !ok || !field.Sortable {
			return errors.NewCode(errors.InvalidInput, "invalid sort field").
				WithContext("field", sort.Field)
		}
	}
	return nil
}

// ValidateFields 校验字段投影能力。
func (s *QuerySchema) ValidateFields(fields []string) error {
	if len(fields) == 0 || s == nil {
		return nil
	}
	for _, name := range fields {
		field, ok := s.Field(name)
		if !ok || !field.Selectable {
			return errors.NewCode(errors.InvalidInput, "invalid field selection").
				WithContext("field", name)
		}
	}
	return nil
}
