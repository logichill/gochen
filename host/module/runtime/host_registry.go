package runtime

import (
	"reflect"

	"gochen/di"
	"gochen/errors"
)

func (s *Host) ensureFrameworkRegistries() error {
	if s == nil {
		return nil
	}
	s.config = ensureHostConfig(s.config)
	if s.container == nil {
		return errors.NewCode(errors.Internal, "container is nil")
	}

	if s.authzRegistry == nil {
		s.authzRegistry = s.config.AuthzRegistry
	}
	if s.moduleRegistry == nil {
		s.moduleRegistry = s.config.ModuleRegistry
	}
	if s.metadataRegistry == nil {
		s.metadataRegistry = s.config.MetadataRegistry
	}

	if err := registerFrameworkRegistry(s.container, s.authzRegistry); err != nil {
		return err
	}
	if err := registerFrameworkRegistry(s.container, s.moduleRegistry); err != nil {
		return err
	}
	if err := registerFrameworkRegistry(s.container, s.metadataRegistry); err != nil {
		return err
	}

	return nil
}

func registerFrameworkRegistry(container di.IContainer, instance any) error {
	if container == nil || instance == nil {
		return nil
	}
	serviceType := reflect.TypeOf(instance)
	if serviceType == nil || container.IsRegistered(serviceType) {
		return nil
	}
	return container.RegisterInstance(serviceType, di.NewInstance(instance))
}
