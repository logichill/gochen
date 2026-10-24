package query

import (
	"math"
	"strconv"
	"strings"
	"time"

	"gochen/errors"
)

func (f QueryField) normalizeValues(values []string) ([]string, []QueryValue, error) {
	if len(values) == 0 {
		return nil, nil, errors.NewCode(errors.InvalidInput, "empty filter value list").
			WithContext("field", f.Name)
	}
	out := make([]string, 0, len(values))
	decoded := make([]QueryValue, 0, len(values))
	for _, value := range values {
		normalized, queryValue, err := f.normalizeValue(value)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, normalized)
		decoded = append(decoded, queryValue)
	}
	return out, decoded, nil
}

func (f QueryField) normalizeValue(value string) (string, QueryValue, error) {
	switch f.Type {
	case FieldTypeInt:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid int filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatInt(parsed, 10)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Int: parsed}, nil
	case FieldTypeFloat:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid float filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid non-finite float filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatFloat(parsed, 'g', -1, 64)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Float: parsed}, nil
	case FieldTypeBool:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid bool filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatBool(parsed)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Bool: parsed}, nil
	case FieldTypeTime:
		layout := strings.TrimSpace(f.TimeLayout)
		if layout == "" {
			layout = time.RFC3339Nano
		}
		parsed, err := time.Parse(layout, strings.TrimSpace(value))
		if err != nil && f.TimeLayout == "" {
			parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
		}
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid time filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		if f.TimeLayout != "" {
			normalized := parsed.Format(f.TimeLayout)
			return normalized, QueryValue{Type: f.Type, Normalized: normalized, Time: parsed}, nil
		}
		normalized := parsed.Format(time.RFC3339Nano)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Time: parsed}, nil
	default:
		return value, QueryValue{Type: f.Type, Normalized: value, String: value}, nil
	}
}
