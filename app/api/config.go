// Package api 提供 RESTful API 路由构建功能
package api

import (
	"net/http"

	"gochen/errors"
	core "gochen/httpx"
	bhttp "gochen/httpx/basic"
)

// RouteConfig 路由配置
type RouteConfig struct {
	// 基础路径
	BasePath string

	// 是否启用批量操作
	EnableBatch bool

	// 是否启用分页查询
	EnablePagination bool

	// 自定义验证器
	Validator func(interface{}) error

	// 自定义错误处理器
	ErrorHandler func(error) core.IResponse

	// 最大分页大小
	MaxPageSize int

	// 默认分页大小
	DefaultPageSize int

	// 请求体大小限制
	MaxBodySize int64

	// 允许的 HTTP 方法
	AllowedMethods []string

	// CORS 配置
	CORS *CORSConfig

	// 中间件
	Middlewares []core.Middleware

	// 响应包装器
	ResponseWrapper func(data interface{}) interface{}
}

// CORSConfig CORS 配置
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultRouteConfig 默认路由配置
func DefaultRouteConfig() *RouteConfig {
	return &RouteConfig{
		EnableBatch:      true,
		EnablePagination: true,
		MaxPageSize:      1000,
		DefaultPageSize:  10,
		MaxBodySize:      10 << 20, // 10MB
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		ErrorHandler:     DefaultErrorHandler,
		ResponseWrapper:  DefaultResponseWrapper,
		CORS: &CORSConfig{
			AllowOrigins:     []string{"*"},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"*"},
			AllowCredentials: false,
			MaxAge:           86400,
		},
	}
}

// DefaultErrorHandler 默认错误处理器
func DefaultErrorHandler(err error) core.IResponse {
	if err == nil {
		return bhttp.SuccessResponse(nil)
	}

	// 先将领域/事件存储/命令总线等错误规范化为 AppError
	err = errors.Normalize(err)

	// 根据错误类型返回不同的响应
	switch {
	case errors.IsValidation(err):
		return bhttp.ErrorResponse(http.StatusBadRequest, err.Error())
	case errors.IsNotFound(err):
		return bhttp.ErrorResponse(http.StatusNotFound, err.Error())
	case errors.IsConflict(err):
		return bhttp.ErrorResponse(http.StatusConflict, err.Error())
	case errors.IsErrorCode(err, errors.ErrCodeUnauthorized):
		return bhttp.ErrorResponse(http.StatusUnauthorized, err.Error())
	case errors.IsErrorCode(err, errors.ErrCodeForbidden):
		return bhttp.ErrorResponse(http.StatusForbidden, err.Error())
	case errors.IsErrorCode(err, errors.ErrCodeTooManyRequests):
		return bhttp.ErrorResponse(http.StatusTooManyRequests, err.Error())
	default:
		return bhttp.ErrorResponse(http.StatusInternalServerError, "内部服务器错误")
	}
}

// DefaultResponseWrapper 默认响应包装器
func DefaultResponseWrapper(data interface{}) interface{} {
	return map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    data,
	}
}
