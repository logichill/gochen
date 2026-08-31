package outbox

import (
	"reflect"

	"gochen/errors"
	"gochen/eventing/bus"
)

func validatePublisherDependencies[ID comparable](repo IOutboxRepository[ID], eventBus bus.IEventBus) error {
	if isNilDependency(repo) {
		return errors.NewCode(errors.InvalidInput, "outbox repository cannot be nil")
	}
	if isNilDependency(eventBus) {
		return errors.NewCode(errors.InvalidInput, "event bus cannot be nil")
	}
	return nil
}

func isNilDependency(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
