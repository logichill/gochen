package di

import (
	"sync"
	"testing"
)

// 测试用接口和实现
type ITestService interface {
	GetName() string
}

type TestServiceImpl struct {
	name string
}

func (s *TestServiceImpl) GetName() string {
	return s.name
}

type IDatabase interface {
	Connect() error
}

type MockDatabase struct {
	connected bool
}

func (db *MockDatabase) Connect() error {
	db.connected = true
	return nil
}

// TestNew 测试容器创建
func TestNew(t *testing.T) {
	container := New()
	if container == nil {
		t.Fatal("容器创建失败")
	}
	if container.services == nil {
		t.Fatal("容器的services map未初始化")
	}
}

// TestRegister 测试服务注册
func TestRegister(t *testing.T) {
	container := New()

	service := &TestServiceImpl{name: "test"}
	err := container.Register(service)
	if err != nil {
		t.Fatalf("注册服务失败: %v", err)
	}

	// 验证服务已注册
	if !container.Has((*TestServiceImpl)(nil)) {
		t.Error("服务未正确注册")
	}
}

// TestRegister_Nil 测试注册nil服务
func TestRegister_Nil(t *testing.T) {
	container := New()

	err := container.Register(nil)
	if err == nil {
		t.Error("注册nil服务应该返回错误")
	}
}

// TestRegisterAs 测试以接口类型注册服务
func TestRegisterAs(t *testing.T) {
	container := New()

	service := &TestServiceImpl{name: "test"}
	err := container.RegisterAs((*ITestService)(nil), service)
	if err != nil {
		t.Fatalf("注册服务失败: %v", err)
	}

	// 验证可以通过接口类型解析
	resolved, err := container.Resolve((*ITestService)(nil))
	if err != nil {
		t.Fatalf("解析服务失败: %v", err)
	}

	resolvedService, ok := resolved.(ITestService)
	if !ok {
		t.Fatal("解析的服务类型不正确")
	}

	if resolvedService.GetName() != "test" {
		t.Errorf("服务方法返回错误: got %s, want test", resolvedService.GetName())
	}
}

// TestRegisterAs_Nil 测试以接口类型注册nil服务
func TestRegisterAs_Nil(t *testing.T) {
	container := New()

	err := container.RegisterAs((*ITestService)(nil), nil)
	if err == nil {
		t.Error("注册nil服务应该返回错误")
	}
}

// TestResolve 测试服务解析
func TestResolve(t *testing.T) {
	container := New()

	original := &TestServiceImpl{name: "original"}
	err := container.Register(original)
	if err != nil {
		t.Fatalf("注册服务失败: %v", err)
	}

	resolved, err := container.Resolve((*TestServiceImpl)(nil))
	if err != nil {
		t.Fatalf("解析服务失败: %v", err)
	}

	resolvedService, ok := resolved.(*TestServiceImpl)
	if !ok {
		t.Fatal("解析的服务类型不正确")
	}

	if resolvedService.GetName() != "original" {
		t.Errorf("解析的服务不是原始服务")
	}

	// 验证是同一个实例
	if resolvedService != original {
		t.Error("解析的服务不是同一个实例")
	}
}

// TestResolve_NotFound 测试解析不存在的服务
func TestResolve_NotFound(t *testing.T) {
	container := New()

	_, err := container.Resolve((*TestServiceImpl)(nil))
	if err == nil {
		t.Error("解析不存在的服务应该返回错误")
	}
}

// TestMustResolve 测试MustResolve正常情况
func TestMustResolve(t *testing.T) {
	container := New()

	service := &TestServiceImpl{name: "test"}
	container.Register(service)

	resolved := container.MustResolve((*TestServiceImpl)(nil))
	if resolved == nil {
		t.Fatal("MustResolve返回了nil")
	}

	resolvedService, ok := resolved.(*TestServiceImpl)
	if !ok {
		t.Fatal("MustResolve返回的类型不正确")
	}

	if resolvedService.name != "test" {
		t.Errorf("MustResolve返回的服务不正确")
	}
}

// TestMustResolve_Panic 测试MustResolve在服务不存在时panic
func TestMustResolve_Panic(t *testing.T) {
	container := New()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustResolve在服务不存在时应该panic")
		}
	}()

	container.MustResolve((*TestServiceImpl)(nil))
}

// TestHas 测试Has方法
func TestHas(t *testing.T) {
	container := New()

	// 服务不存在
	if container.Has((*TestServiceImpl)(nil)) {
		t.Error("Has返回true但服务不存在")
	}

	// 注册服务
	service := &TestServiceImpl{name: "test"}
	container.Register(service)

	// 服务存在
	if !container.Has((*TestServiceImpl)(nil)) {
		t.Error("Has返回false但服务已注册")
	}
}

// TestConcurrent 测试并发注册和解析
func TestConcurrent(t *testing.T) {
	container := New()

	const goroutines = 10
	const operations = 100

	var wg sync.WaitGroup

	// 并发注册
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				service := &TestServiceImpl{name: "test"}
				err := container.Register(service)
				if err != nil {
					t.Errorf("并发注册失败: %v", err)
				}
			}
		}(i)
	}

	// 并发解析
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				container.Resolve((*TestServiceImpl)(nil))
			}
		}()
	}

	// 并发Has检查
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				container.Has((*TestServiceImpl)(nil))
			}
		}()
	}

	wg.Wait()
}

// TestMultipleServices 测试注册和解析多个服务
func TestMultipleServices(t *testing.T) {
	container := New()

	service1 := &TestServiceImpl{name: "service1"}
	service2 := &MockDatabase{connected: false}

	container.Register(service1)
	container.Register(service2)

	// 解析第一个服务
	resolved1, err := container.Resolve((*TestServiceImpl)(nil))
	if err != nil {
		t.Fatalf("解析service1失败: %v", err)
	}
	if resolved1.(*TestServiceImpl).name != "service1" {
		t.Error("service1解析错误")
	}

	// 解析第二个服务
	resolved2, err := container.Resolve((*MockDatabase)(nil))
	if err != nil {
		t.Fatalf("解析service2失败: %v", err)
	}
	if resolved2.(*MockDatabase).connected {
		t.Error("service2状态错误")
	}
}

// TestServiceOverride 测试服务覆盖
func TestServiceOverride(t *testing.T) {
	container := New()

	// 注册第一个服务
	service1 := &TestServiceImpl{name: "first"}
	container.Register(service1)

	// 覆盖注册第二个服务
	service2 := &TestServiceImpl{name: "second"}
	container.Register(service2)

	// 解析应该返回第二个服务
	resolved, err := container.Resolve((*TestServiceImpl)(nil))
	if err != nil {
		t.Fatalf("解析服务失败: %v", err)
	}

	if resolved.(*TestServiceImpl).name != "second" {
		t.Errorf("期望服务被覆盖为'second'，但得到'%s'", resolved.(*TestServiceImpl).name)
	}
}

// TestGlobalContainer 测试全局容器功能
func TestGlobalContainer(t *testing.T) {
	// 注意：全局容器在整个测试过程中共享，可能影响其他测试
	// 这里我们测试基本功能

	service := &TestServiceImpl{name: "global"}

	err := RegisterGlobal(service)
	if err != nil {
		t.Fatalf("注册到全局容器失败: %v", err)
	}

	// 使用全局解析
	resolved, err := ResolveGlobal((*TestServiceImpl)(nil))
	if err != nil {
		t.Fatalf("从全局容器解析失败: %v", err)
	}

	if resolved.(*TestServiceImpl).name != "global" {
		t.Error("全局容器解析的服务不正确")
	}
}

// TestGlobalContainerAs 测试全局容器的RegisterAs功能
func TestGlobalContainerAs(t *testing.T) {
	service := &TestServiceImpl{name: "global-interface"}

	err := RegisterAsGlobal((*ITestService)(nil), service)
	if err != nil {
		t.Fatalf("注册到全局容器失败: %v", err)
	}

	resolved, err := ResolveGlobal((*ITestService)(nil))
	if err != nil {
		t.Fatalf("从全局容器解析失败: %v", err)
	}

	if resolved.(ITestService).GetName() != "global-interface" {
		t.Error("全局容器解析的服务不正确")
	}
}

// TestMustResolveGlobal 测试全局容器的MustResolve
func TestMustResolveGlobal(t *testing.T) {
	service := &TestServiceImpl{name: "must-global"}
	RegisterGlobal(service)

	resolved := MustResolveGlobal((*TestServiceImpl)(nil))
	if resolved.(*TestServiceImpl).name != "must-global" {
		t.Error("MustResolveGlobal返回的服务不正确")
	}
}

// TestInterfaceImplementation 测试接口实现注册
func TestInterfaceImplementation(t *testing.T) {
	container := New()

	// 注册实现为接口类型
	impl := &TestServiceImpl{name: "impl"}
	container.RegisterAs((*ITestService)(nil), impl)

	// 解析接口
	service, err := container.Resolve((*ITestService)(nil))
	if err != nil {
		t.Fatalf("解析接口失败: %v", err)
	}

	// 验证可以调用接口方法
	testService := service.(ITestService)
	name := testService.GetName()
	if name != "impl" {
		t.Errorf("接口方法调用错误: got %s, want impl", name)
	}
}

// BenchmarkRegister 基准测试：注册服务性能
func BenchmarkRegister(b *testing.B) {
	container := New()
	service := &TestServiceImpl{name: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.Register(service)
	}
}

// BenchmarkResolve 基准测试：解析服务性能
func BenchmarkResolve(b *testing.B) {
	container := New()
	service := &TestServiceImpl{name: "bench"}
	container.Register(service)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.Resolve((*TestServiceImpl)(nil))
	}
}

// BenchmarkHas 基准测试：Has检查性能
func BenchmarkHas(b *testing.B) {
	container := New()
	service := &TestServiceImpl{name: "bench"}
	container.Register(service)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.Has((*TestServiceImpl)(nil))
	}
}

// BenchmarkConcurrentResolve 基准测试：并发解析性能
func BenchmarkConcurrentResolve(b *testing.B) {
	container := New()
	service := &TestServiceImpl{name: "bench"}
	container.Register(service)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			container.Resolve((*TestServiceImpl)(nil))
		}
	})
}
