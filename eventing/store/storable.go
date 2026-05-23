package store

import "gochen/eventing"

// ToStorable 转换Storable。
func ToStorable[ID comparable](events []eventing.Event[ID]) []eventing.IStorableEvent[ID] {
	if len(events) == 0 {
		return nil
	}
	res := make([]eventing.IStorableEvent[ID], len(events))
	for i := range events {
		e := events[i]
		res[i] = &e
	}
	return res
}
