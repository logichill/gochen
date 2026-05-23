package httpx

import (
	"fmt"
	"strconv"

	"gochen/errors"
)

// ParseInt64Param 从 HTTP 路径参数中解析正整数 ID。
func ParseInt64Param(ctx IContext, paramName string) (int64, error) {
	if ctx == nil {
		return 0, errors.NewCode(errors.InvalidInput, "http context cannot be nil")
	}
	idStr := ctx.Param(paramName)
	if idStr == "" {
		return 0, errors.NewCode(errors.InvalidInput, fmt.Sprintf("parameter %s cannot be empty", paramName))
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, errors.Wrap(err, errors.InvalidInput, fmt.Sprintf("parameter %s must be a valid integer", paramName))
	}
	if id <= 0 {
		return 0, errors.NewCode(errors.InvalidInput, fmt.Sprintf("parameter %s must be greater than 0", paramName))
	}
	return id, nil
}
