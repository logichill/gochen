package httpx

import (
	"strings"
)

// NormalizeRoutePrefix 规范化路由前缀。
func NormalizeRoutePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}

// NormalizeRoutePath 规范化路由路径。
func NormalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// JoinRoutePrefix 拼接父前缀和子前缀。
func JoinRoutePrefix(parentPrefix string, childPrefix string) string {
	parentPrefix = NormalizeRoutePrefix(parentPrefix)
	childPrefix = NormalizeRoutePrefix(childPrefix)
	if parentPrefix == "" {
		return childPrefix
	}
	if childPrefix == "" {
		return parentPrefix
	}
	return parentPrefix + childPrefix
}

// JoinRoutePath 拼接路由前缀与子路径，严格保留子路径指定的尾部斜杠语义。
func JoinRoutePath(prefix string, path string) string {
	prefix = NormalizeRoutePrefix(prefix)
	path = strings.TrimSpace(path)
	if prefix == "" {
		if path == "" {
			return "/"
		}
		if !strings.HasPrefix(path, "/") {
			return "/" + path
		}
		return path
	}
	if path == "" {
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return prefix + path
}
