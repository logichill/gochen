package request

import (
	"bytes"
	"encoding/json"
	stdErrors "errors"
	"io"
	"net/http"

	"gochen/errors"
	"gochen/httpx"
)

// ReadBody 读取并缓存请求体，统一处理 body size 限制与重复读取。
func ReadBody(w http.ResponseWriter, r *http.Request, values map[string]any, cacheKey string) ([]byte, error) {
	if values != nil {
		if data, ok := valueAs[[]byte](values[cacheKey]); ok {
			return data, nil
		}
	}

	if values != nil {
		if v, ok := values[httpx.MaxBodySizeKey]; ok {
			switch limit, ok := valueAs[int64](v); {
			case ok:
				if limit > 0 {
					r.Body = http.MaxBytesReader(w, r.Body, limit)
				}
			default:
				if limitInt, ok := valueAs[int](v); ok && limitInt > 0 {
					r.Body = http.MaxBytesReader(w, r.Body, int64(limitInt))
				} else {
					r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBodySizeBytes)
				}
			}
		} else {
			r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBodySizeBytes)
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBodySizeBytes)
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if stdErrors.As(err, &maxErr) {
			return nil, errors.NewCode(errors.PayloadTooLarge, "request body too large")
		}
		return nil, errors.Wrap(err, errors.Internal, "failed to read request body")
	}
	_ = r.Body.Close()
	if values != nil {
		values[cacheKey] = httpx.ValueOf(data)
	}
	return data, nil
}

// BindJSON 使用统一的严格 JSON 语义绑定请求体。
func BindJSON(w http.ResponseWriter, r *http.Request, values map[string]any, cacheKey string, obj any) error {
	if obj == nil {
		return errors.NewCode(errors.InvalidInput, "bind target cannot be nil")
	}
	body, err := ReadBody(w, r, values, cacheKey)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(obj); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to parse JSON")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.NewCode(errors.InvalidInput, "failed to parse JSON: trailing data")
		}
		return errors.Wrap(err, errors.InvalidInput, "failed to parse JSON: trailing data")
	}
	return nil
}

func valueAs[T any](value any) (T, bool) {
	var zero T
	if typed, ok := value.(T); ok {
		return typed, true
	}
	if wrapped, ok := value.(httpx.ContextValue); ok {
		return httpx.ValueAs[T](wrapped)
	}
	return zero, false
}

// BindQuery 把查询参数绑定到结构体目标。
func BindQuery(target any, values map[string][]string) error {
	if target == nil {
		return errors.NewCode(errors.InvalidInput, "bind target cannot be nil")
	}
	return bindURLValues(target, values)
}
