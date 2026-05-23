package rest

import (
	"sort"
	"strings"

	"gochen/errors"
	"gochen/httpx"
)

// RejectLegacyQueryParams 在 QuerySchema 路径上显式拒绝旧版扁平 query 参数，
// 避免请求“成功但过滤失效”地静默退化。
func RejectLegacyQueryParams(ctx httpx.IContext, keys ...string) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return RejectLegacyParams(ctx.QueryParams(), keys...)
}

// RejectLegacyParams 便于在单测里直接针对 query map 复用相同规则。
func RejectLegacyParams(params map[string][]string, keys ...string) error {
	if len(params) == 0 || len(keys) == 0 {
		return nil
	}

	var found []string
	for _, key := range keys {
		if values, ok := params[key]; ok && len(values) > 0 {
			found = append(found, key)
		}
	}
	if len(found) == 0 {
		return nil
	}

	sort.Strings(found)
	return errors.NewCode(errors.InvalidInput, "legacy query params are no longer supported; use filter/page/size DSL").
		WithContext("legacy_params", strings.Join(found, ","))
}
