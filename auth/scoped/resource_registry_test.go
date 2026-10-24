package scoped

import (
	"sync"
	"testing"
)

type staticResolver struct {
	resource Resource
}

func (s staticResolver) Resolve(target any) (Resource, bool) { return s.resource, true }

// P2-054/P2-068 回归：Resolve 与并发 Register 不得读写共享底层数组。
func TestResourceRegistryResolveConcurrentRegister(t *testing.T) {
	registry := NewResourceRegistry(staticResolver{resource: Resource{Kind: "base"}})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			registry.Register(staticResolver{resource: Resource{Kind: "extra"}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			if _, ok := registry.Resolve(struct{}{}); !ok {
				t.Errorf("resolve failed")
				return
			}
		}
	}()
	wg.Wait()
}
