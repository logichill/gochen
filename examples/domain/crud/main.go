package main

import (
	"context"
	"log"
	"time"

	"gochen/app/api"
	application "gochen/app/application"
	"gochen/examples/internal/mocks"
	httpx "gochen/http"
)

// User 简单实体（普通 CRUD）
type User struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Version uint64 `json:"version"`
	Active  bool   `json:"active"`
	// 扩展示例字段
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
}

func (u *User) GetID() int64          { return u.ID }
func (u *User) GetVersion() uint64    { return u.Version }
func (u *User) GetCreatedAt() int64   { return 0 }
func (u *User) GetUpdatedAt() int64   { return 0 }
func (u *User) SetUpdatedAt(_ int64)  {}
func (u *User) IsDeleted() bool       { return false }
func (u *User) MarkAsDeleted()        {}
func (u *User) Restore()              {}
func (u *User) GetDeletedAt() *int64  { return nil }
func (u *User) Validate() error       { return nil }
func (u *User) SetID(id int64)        { u.ID = id }
func (u *User) GetEntityType() string { return "User" }

// 扩展仓储：增加激活方法
type ExtUserRepo struct {
	*mocks.GenericMockRepository[*User]
}

func (r *ExtUserRepo) Activate(ctx context.Context, id int64) error {
	u, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if u.Active {
		return nil
	}
	now := time.Now()
	u.Active = true
	u.ActivatedAt = &now
	u.SetUpdatedAt(now.Unix())
	return r.Update(ctx, u)
}

// 扩展服务：封装业务方法
type UserServiceExt struct {
	application.IApplication[*User]
	repo *ExtUserRepo
}

func NewUserServiceExt(repo *ExtUserRepo, base application.IApplication[*User]) *UserServiceExt {
	return &UserServiceExt{IApplication: base, repo: repo}
}

func (s *UserServiceExt) Activate(ctx context.Context, id int64) error {
	return s.repo.Activate(ctx, id)
}

func main() {
	log.Println("=== 普通 CRUD 示例 ===")

	// 1) 依赖（内存模拟）
	baseRepo := mocks.NewGenericMockRepository[*User]()
	userRepo := &ExtUserRepo{GenericMockRepository: baseRepo}
	validator := mocks.NewGenericValidator(func(e *User) error { return e.Validate() })
	router := mocks.NewMockRouter()

	// 2) 服务
	baseSvc := application.NewApplication(userRepo, validator, nil)
	svc := NewUserServiceExt(userRepo, baseSvc)

	// 3) 注册 RESTful API
	if err := api.RegisterRESTfulAPI(router, svc, validator); err != nil {
		log.Fatalf("注册 API 失败: %v", err)
	}

	// 3.1 注册一个扩展业务方法：激活用户（示例）
	// 说明：仅演示自定义业务路由的注册方式，处理器返回 nil 以便专注示例结构
	router.POST("/users/:id/activate", func(ctx httpx.IHttpContext) error { return svc.Activate(context.Background(), 1) })
	router.PrintRoutes()

	// 4) 演示创建/查询/分页
	if err := svc.Create(context.Background(), &User{Name: "张三", Email: "zhangsan@example.com"}); err != nil {
		log.Fatalf("创建用户失败: %v", err)
	}
	got, err := svc.Get(context.Background(), 1)
	if err != nil {
		log.Fatalf("查询用户失败: %v", err)
	}
	log.Printf("查询用户: %+v", got)
	page, err := svc.ListPage(context.Background(), &application.PaginationOptions{Page: 1, Size: 10})
	if err != nil {
		log.Fatalf("分页查询失败: %v", err)
	}
	log.Printf("分页: total=%d, items=%d", page.Total, len(page.Data))
}
