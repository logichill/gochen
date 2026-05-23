package main

import (
	"context"
	"fmt"
	"log"

	"gochen/auth"
	"gochen/examples/internal/mocks"
	"gochen/host"
	"gochen/httpx"
)

// 这个示例展示模块化应用的推荐接入路径：
// host.Module 声明 provider、路由注册器、权限目录和资源解析器，
// host.Run 负责 DI 装配、模块 Init、路由挂载和启动期冲突检查。
var catalogPermissions = auth.NewAPIPermissionSet(
	"catalog",
	auth.PermissionActionRead,
	auth.PermissionActionWrite,
)

type catalogItem struct {
	ID       string
	TenantID string
}

type catalogService struct{}

func NewCatalogService() *catalogService {
	return &catalogService{}
}

type catalogRoutes struct {
	service *catalogService
}

func NewCatalogRoutes(service *catalogService) *catalogRoutes {
	return &catalogRoutes{service: service}
}

func (r *catalogRoutes) RegisterRoutes(group httpx.IRouteGroup) error {
	if r == nil || r.service == nil {
		return fmt.Errorf("catalog routes require service")
	}
	group.GET("/items", func(c httpx.IContext) error {
		return c.JSON(200, httpx.JSONValue(map[string]any{"items": []string{"book", "pen"}}))
	})
	return nil
}

func NewCatalogModule() (host.IModule, error) {
	return host.Module("catalog").
		Name("Catalog").
		Provide(NewCatalogService).
		RouteRegistrar(NewCatalogRoutes).
		PermissionDefinitions(catalogPermissions.Definitions()...).
		ResourceResolver(
			auth.TypedResourceResolver[*catalogItem](func(item *catalogItem) (auth.Resource, bool) {
				if item == nil {
					return auth.Resource{}, false
				}
				return auth.Resource{
					Kind:     "catalog.item",
					ID:       item.ID,
					TenantID: item.TenantID,
				}, true
			}),
		).
		Build()
}

func main() {
	log.SetFlags(0)

	server := mocks.NewMockServer()
	registry := auth.NewRegistry()

	if err := host.Run(context.Background(),
		host.WithName("catalog-service"),
		host.WithHTTPServer(server),
		host.WithAuthzRegistry(registry),
		host.WithBasePath("/api/v1"),
		host.WithModuleHTTP("catalog", host.ModuleHTTPConfig{Prefix: "/catalog"}),
		host.WithModules(NewCatalogModule),
		host.WithFailFastOnRouteConflicts(true),
	); err != nil {
		log.Fatal(err)
	}

	log.Println("routes:")
	for _, route := range server.Routes {
		log.Printf("  %s %s", route.Method, route.Path)
	}

	log.Println("permissions:")
	for _, permission := range registry.Permissions() {
		log.Printf("  %s", permission)
	}

	resource, ok := registry.Resolve(&catalogItem{ID: "item-1", TenantID: "tenant-a"})
	log.Printf("resource resolved: %t %#v", ok, resource)
}
