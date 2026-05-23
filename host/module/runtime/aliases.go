package runtime

import (
	deventsourced "gochen/domain/eventsourced"
	"gochen/host/module"
	moduleasm "gochen/host/module/assembly"
	"gochen/host/module/runtimecap"
)

type IModule = module.IModule
type IRouteModule = module.IRouteModule
type IModuleDependencyProvider = module.IModuleDependencyProvider
type IModuleAuthzProvider = moduleasm.IModuleAuthzProvider
type ModuleCtor = module.ModuleCtor
type ModuleStopFunc = module.ModuleStopFunc
type ModuleInitOptions = module.ModuleInitOptions
type ModuleHTTPOptions = runtimecap.ModuleHTTPOptions
type ModuleInfo = module.ModuleInfo
type ModuleRegistry = module.ModuleRegistry
type MetadataRegistry = deventsourced.MetadataRegistry

func BuildModules(ctors ...ModuleCtor) ([]IModule, error) { return module.BuildModules(ctors...) }

func NewModuleRegistry() *ModuleRegistry { return module.NewModuleRegistry() }

func NewMetadataRegistry() *MetadataRegistry { return deventsourced.NewMetadataRegistry() }

func normalizeModuleID(raw string) (string, error) { return module.NormalizeModuleID(raw) }
