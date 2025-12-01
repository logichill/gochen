// Package di 提供简单的依赖注入容器。
//
// 注意：本包暴露的全局容器（globalContainer 及 RegisterGlobal/ResolveGlobal/MustResolveGlobal）
// 仅推荐用于快速原型、demo、示例程序或遗留代码迁移过程。
// 在生产代码中，应优先通过显式注入的方式使用依赖：
// - 在应用启动阶段构造并传递一个实现了 IContainer 的容器实例；
// - 通过构造函数参数将依赖传入业务对象，而不是在函数内部直接访问全局容器。
// 直接依赖全局容器会引入全局状态风险，包括但不限于：
// - 测试隔离困难（不同测试用例共享同一容器状态）；
// - 对启动顺序产生隐式依赖（必须按特定顺序注册服务）；
// - 隐式依赖难以追踪（调用方无法从函数签名看出所需依赖）。
package di

import (
	"fmt"
	"gochen/errors"
	"reflect"
	"sync"
)

// IContainer 应用层依赖注入容器接口（项目统一接口）
//
// 注意：这是框架级接口定义，具体实现可在应用内部（如 internal/di）
// 提供更强的适配版本；本文件同时提供最简实现 BasicContainer。
type IContainer interface {
	// 注册依赖提供者（构造函数），以第一个返回值类型名作为服务名
	RegisterConstructor(constructor any) error

	// 注册单例
	RegisterSingleton(name string, factory any) error

	// 注册瞬态（最简实现按单例处理）
	RegisterTransient(name string, factory any) error

	// 注册实例
	RegisterInstance(name string, instance any) error

	// 解析依赖
	Resolve(name string) (any, error)

	// 解析到指定类型
	ResolveTo(name string, target any) error

	// 检查是否已注册
	IsRegistered(name string) bool

	// 获取所有注册的服务名称
	GetRegisteredNames() []string

	// 调用函数并按参数类型注入
	Invoke(function any) error

	// 清空容器
	Clear()
}

// Container 依赖注入容器
type Container struct {
	services map[reflect.Type]any
	mutex    sync.RWMutex
}

// New 创建容器
func New() *Container {
	return &Container{
		services: make(map[reflect.Type]any),
	}
}

// Register 注册服务
// 注意：service 必须是指针类型，会自动提取元素类型作为key
func (c *Container) Register(service any) error {
	if service == nil {
		return fmt.Errorf("service cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 获取指针指向的类型，与Resolve保持一致
	t := reflect.TypeOf(service)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	c.services[t] = service

	return nil
}

// RegisterAs 注册服务并指定接口类型
func (c *Container) RegisterAs(serviceType any, service any) error {
	if service == nil {
		return fmt.Errorf("service cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	t := reflect.TypeOf(serviceType).Elem()
	c.services[t] = service

	return nil
}

// Resolve 解析服务
func (c *Container) Resolve(serviceType any) (any, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	t := reflect.TypeOf(serviceType).Elem()
	service, exists := c.services[t]
	if !exists {
		return nil, fmt.Errorf("service not found: %v", t)
	}

	return service, nil
}

// MustResolve 解析服务（panic版本）
func (c *Container) MustResolve(serviceType any) any {
	service, err := c.Resolve(serviceType)
	if err != nil {
		panic(err)
	}
	return service
}

// Has 检查服务是否存在
func (c *Container) Has(serviceType any) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	t := reflect.TypeOf(serviceType).Elem()
	_, exists := c.services[t]
	return exists
}

// 全局容器
var globalContainer = New()

// RegisterGlobal 注册到全局容器
func RegisterGlobal(service any) error {
	return globalContainer.Register(service)
}

// RegisterAsGlobal 注册到全局容器（指定接口）
func RegisterAsGlobal(serviceType any, service any) error {
	return globalContainer.RegisterAs(serviceType, service)
}

// ResolveGlobal 从全局容器解析
func ResolveGlobal(serviceType any) (any, error) {
	return globalContainer.Resolve(serviceType)
}

// MustResolveGlobal 从全局容器解析（panic版本）
func MustResolveGlobal(serviceType any) any {
	return globalContainer.MustResolve(serviceType)
}

// BasicContainer 一个最简 IContainer 实现
// 说明：
// - 使用字符串 name 作为键
// - RegisterTransient 等价于 RegisterSingleton（保持简单）
// - constructor 的服务名取第一个返回值类型字符串
type BasicContainer struct {
	services  map[string]any
	instances map[string]any
	mutex     sync.RWMutex
}

// NewBasic 创建最简容器
func NewBasic() *BasicContainer {
	return &BasicContainer{
		services:  make(map[string]any),
		instances: make(map[string]any),
	}
}

func (c *BasicContainer) RegisterConstructor(constructor any) error {
	if constructor == nil {
		return errors.NewError(errors.ErrCodeInvalidInput, "constructor cannot be nil")
	}
	t := reflect.TypeOf(constructor)
	if t.Kind() != reflect.Func {
		return errors.NewError(errors.ErrCodeInvalidInput, "parameter must be a function")
	}
	if t.NumOut() == 0 {
		return errors.NewError(errors.ErrCodeInvalidInput, "constructor must have a return value")
	}
	name := t.Out(0).String()
	return c.RegisterSingleton(name, constructor)
}

func (c *BasicContainer) RegisterSingleton(name string, factory any) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, exists := c.services[name]; exists {
		return errors.NewError(errors.ErrCodeConflict, fmt.Sprintf("service %s already registered", name))
	}
	c.services[name] = factory
	return nil
}

func (c *BasicContainer) RegisterTransient(name string, factory any) error {
	return c.RegisterSingleton(name, factory)
}

func (c *BasicContainer) RegisterInstance(name string, instance any) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, exists := c.services[name]; exists {
		return errors.NewError(errors.ErrCodeConflict, fmt.Sprintf("service %s already registered", name))
	}
	c.instances[name] = instance
	c.services[name] = instance
	return nil
}

func (c *BasicContainer) Resolve(name string) (any, error) {
	c.mutex.RLock()
	_, exists := c.services[name]
	c.mutex.RUnlock()
	if !exists {
		return nil, errors.NewError(errors.ErrCodeNotFound, fmt.Sprintf("service %s not registered", name))
	}
	c.mutex.RLock()
	if inst, ok := c.instances[name]; ok {
		c.mutex.RUnlock()
		return inst, nil
	}
	c.mutex.RUnlock()

	c.mutex.Lock()
	factory := c.services[name]
	c.mutex.Unlock()

	inst, err := c.createInstance(factory)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeInternal, fmt.Sprintf("failed to create service %s", name))
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if existing, ok := c.instances[name]; ok {
		return existing, nil
	}
	c.instances[name] = inst
	return inst, nil
}

func (c *BasicContainer) ResolveTo(name string, target any) error {
	inst, err := c.Resolve(name)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.NewError(errors.ErrCodeInvalidInput, "target cannot be nil")
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr {
		return errors.NewError(errors.ErrCodeInvalidInput, "target must be a pointer")
	}
	iv := reflect.ValueOf(inst)
	if !iv.Type().AssignableTo(v.Elem().Type()) {
		return errors.NewError(errors.ErrCodeInvalidInput, fmt.Sprintf("cannot assign %s to %s", iv.Type(), v.Elem().Type()))
	}
	v.Elem().Set(iv)
	return nil
}

func (c *BasicContainer) IsRegistered(name string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	_, ok := c.services[name]
	return ok
}

func (c *BasicContainer) GetRegisteredNames() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	names := make([]string, 0, len(c.services))
	for k := range c.services {
		names = append(names, k)
	}
	return names
}

func (c *BasicContainer) Invoke(function any) error {
	if function == nil {
		return errors.NewError(errors.ErrCodeInvalidInput, "function cannot be nil")
	}
	fv := reflect.ValueOf(function)
	if fv.Type().Kind() != reflect.Func {
		return errors.NewError(errors.ErrCodeInvalidInput, "parameter must be a function")
	}
	args := make([]reflect.Value, fv.Type().NumIn())
	for i := 0; i < fv.Type().NumIn(); i++ {
		paramType := fv.Type().In(i)
		inst, err := c.resolveParameter(paramType)
		if err != nil {
			return errors.WrapError(err, errors.ErrCodeDependency, fmt.Sprintf("failed to resolve parameter %s", paramType))
		}
		args[i] = reflect.ValueOf(inst)
	}
	results := fv.Call(args)
	if len(results) > 0 {
		last := results[len(results)-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !last.IsNil() {
				return errors.WrapError(last.Interface().(error), errors.ErrCodeInternal, "function execution failed")
			}
		}
	}
	return nil
}

func (c *BasicContainer) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.services = make(map[string]any)
	c.instances = make(map[string]any)
}

func (c *BasicContainer) createInstance(factory any) (any, error) {
	fv := reflect.ValueOf(factory)
	ft := fv.Type()
	if ft.Kind() != reflect.Func {
		return factory, nil
	}
	args := make([]reflect.Value, ft.NumIn())
	for i := 0; i < ft.NumIn(); i++ {
		paramType := ft.In(i)
		inst, err := c.resolveParameter(paramType)
		if err != nil {
			return nil, err
		}
		args[i] = reflect.ValueOf(inst)
	}
	results := fv.Call(args)
	if len(results) == 0 {
		return nil, errors.NewError(errors.ErrCodeInternal, "factory function has no return value")
	}
	if len(results) == 2 && !results[1].IsNil() {
		if err, ok := results[1].Interface().(error); ok {
			return nil, errors.WrapError(err, errors.ErrCodeInternal, "factory function execution failed")
		}
	}
	return results[0].Interface(), nil
}

func (c *BasicContainer) resolveParameter(paramType reflect.Type) (any, error) {
	// 先按完整类型名查找
	if c.IsRegistered(paramType.String()) {
		return c.Resolve(paramType.String())
	}
	// 指针元素类型
	if paramType.Kind() == reflect.Ptr {
		if c.IsRegistered(paramType.Elem().String()) {
			return c.Resolve(paramType.Elem().String())
		}
	}
	// 接口名（弱匹配）
	if paramType.Kind() == reflect.Interface {
		if c.IsRegistered(paramType.Name()) {
			return c.Resolve(paramType.Name())
		}
	}
	return nil, errors.NewError(errors.ErrCodeNotFound, fmt.Sprintf("cannot resolve parameter type: %s", paramType))
}
