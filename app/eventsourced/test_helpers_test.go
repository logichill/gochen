package eventsourced

import (
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

func newTestRegistryAndUpgraders() (*registry.Registry, *upcast.UpgraderRegistry) {
	return registry.NewRegistry(), upcast.NewUpgraderRegistry()
}
