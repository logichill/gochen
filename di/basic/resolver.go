package basic

import (
	"reflect"
	"strings"

	"gochen/di"
	"gochen/errors"
)

// resolveParameter 解析Parameter。
func (c *Container) resolveParameter(paramType reflect.Type) (any, error) {
	if paramType == nil {
		return nil, errors.NewCode(errors.InvalidInput, "parameter type is nil")
	}

	c.mutex.RLock()
	key, candidates, ok := c.findRegisteredServiceKeyLocked(paramType)
	c.mutex.RUnlock()
	if ok {
		c.mutex.RLock()
		entry, exists := c.findServiceEntryByKeyLocked(key)
		c.mutex.RUnlock()
		if !exists {
			return nil, errors.NewCode(errors.NotFound, "service not registered").WithContext("service", key)
		}
		return c.resolveEntry(key, entry)
	}
	if len(candidates) > 1 {
		return nil, errors.NewCode(errors.Conflict, "multiple services match parameter type").
			WithContext("parameter_type", di.TypeKey(paramType)).
			WithContext("matches", strings.Join(candidates, ", "))
	}

	return nil, errors.NewCode(errors.NotFound, "cannot resolve parameter type").
		WithContext("parameter_type", di.TypeKey(paramType)).
		WithContext("parameter_type_raw", paramType.String())
}

func serviceOutputType(factory any) reflect.Type {
	if factory == nil {
		return nil
	}
	ft := reflect.TypeOf(factory)
	if ft.Kind() != reflect.Func {
		return ft
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()
	switch ft.NumOut() {
	case 1:
		if ft.Out(0).Implements(errorType) {
			return nil
		}
		return ft.Out(0)
	case 2:
		if !ft.Out(1).Implements(errorType) {
			return nil
		}
		return ft.Out(0)
	default:
		return nil
	}
}

// isCompatibleServiceType 判断Compatible服务类型。
func isCompatibleServiceType(paramType, serviceType reflect.Type) bool {
	if paramType == nil || serviceType == nil {
		return false
	}
	if serviceType.AssignableTo(paramType) {
		return true
	}
	if paramType.Kind() == reflect.Interface && serviceType.Implements(paramType) {
		return true
	}
	return false
}
