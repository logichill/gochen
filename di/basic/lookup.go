package basic

import (
	"reflect"

	"gochen/di"
	"gochen/errors"
)

// Resolve 按类型解析依赖。
func (c *Container) Resolve(serviceType reflect.Type) (any, error) {
	if serviceType == nil {
		return nil, errors.NewCode(errors.InvalidInput, "service type cannot be nil")
	}

	c.mutex.RLock()
	entry, exists := c.findServiceEntryByTypeLocked(serviceType)
	c.mutex.RUnlock()
	if !exists {
		return nil, errors.NewCode(errors.NotFound, "service not registered").WithContext("service_type", di.TypeKey(serviceType))
	}
	return c.resolveEntry(di.TypeKey(serviceType), entry)
}

// IsRegistered 判断某个类型是否已注册。
func (c *Container) IsRegistered(serviceType reflect.Type) bool {
	if serviceType == nil {
		return false
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()
	_, ok := c.findServiceEntryByTypeLocked(serviceType)
	return ok
}

func (c *Container) RegisteredTypes() map[string]reflect.Type {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	out := make(map[string]reflect.Type, len(c.typedServices))
	for serviceType, entry := range c.typedServices {
		if resolvedType := serviceEntryOutputType(entry); resolvedType != nil {
			out[di.TypeKey(serviceType)] = resolvedType
		}
	}
	return out
}

func serviceEntryOutputType(entry *serviceEntry) reflect.Type {
	if entry == nil {
		return nil
	}
	if entry.serviceType != nil {
		return entry.serviceType
	}
	return serviceOutputType(entry.factory)
}

func (c *Container) resolveEntry(serviceLabel string, entry *serviceEntry) (any, error) {
	if entry == nil {
		return nil, errors.NewCode(errors.NotFound, "service not registered").WithContext("service", serviceLabel)
	}

	return c.withResolutionFrame(serviceLabel, func() (any, error) {
		if entry.lifetime == lifetimeTransient {
			inst, err := c.createInstance(entry.factory)
			if err != nil {
				return nil, wrapResolveEntryError(err, serviceLabel)
			}
			return inst, nil
		}

		entry.mu.Lock()
		if entry.created {
			inst := entry.instance
			err := entry.createErr
			entry.mu.Unlock()
			if err != nil {
				return nil, wrapResolveEntryError(err, serviceLabel)
			}
			return inst, nil
		}
		if entry.creating {
			for !entry.created {
				entry.cond.Wait()
			}
			inst := entry.instance
			err := entry.createErr
			entry.mu.Unlock()
			if err != nil {
				return nil, wrapResolveEntryError(err, serviceLabel)
			}
			return inst, nil
		}
		entry.creating = true
		entry.mu.Unlock()

		inst, err := c.createInstance(entry.factory)

		entry.mu.Lock()
		entry.instance = inst
		entry.createErr = err
		entry.created = true
		entry.creating = false
		entry.cond.Broadcast()
		entry.mu.Unlock()

		if err != nil {
			return nil, wrapResolveEntryError(err, serviceLabel)
		}
		return inst, nil
	})
}
