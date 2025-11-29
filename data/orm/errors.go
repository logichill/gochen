package orm

import "errors"

var (
	// ErrNotFound 表示记录未找到。
	ErrNotFound = errors.New("orm: record not found")
	// ErrUnsupported 表示当前适配器不支持请求的能力。
	ErrUnsupported = errors.New("orm: capability unsupported")
)
