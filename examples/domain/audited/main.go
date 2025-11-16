package main

import (
	"context"
	"log"
	"time"

	"gochen/app/api"
	sentity "gochen/domain/entity"
	sservice "gochen/domain/service"
	"gochen/examples/internal/mocks"
	httpx "gochen/httpx"
)

// Article 带审计与软删除的实体
type Article struct {
	sentity.EntityFields
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

func (a *Article) Validate() error { return nil }

// 使用 mocks.NewMockAuditedRepository 提供的通用审计型内存仓储

// ArticleRepo 包装 mocks 审计仓储以添加业务方法
type ArticleRepo struct {
	*mocks.MockAuditedRepository[*Article]
}

// Publish 示例业务方法：发布文章
func (r *ArticleRepo) Publish(ctx context.Context, id int64, by string) error {
	art, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if art.Status == "published" {
		return nil
	}
	now := time.Now()
	art.Status = "published"
	art.PublishedAt = &now
	// 更新审计信息并持久化
	art.SetUpdatedInfo(by, now)
	return r.Update(ctx, art)
}

// ArticleService 演示如何在通用审计服务之上扩展领域能力
type ArticleService struct {
	sservice.IAuditedService[*Article, int64]
	repo *ArticleRepo
}

func NewArticleService(repo *ArticleRepo) *ArticleService {
	base := sservice.NewAuditedService[*Article, int64](repo)
	return &ArticleService{IAuditedService: base, repo: repo}
}

func (s *ArticleService) Publish(ctx context.Context, id int64, by string) error {
	return s.repo.Publish(ctx, id, by)
}

func main() {
	log.Println("=== 审计日志 Demo ===")
	baseRepo := mocks.NewMockAuditedRepository[*Article]()
	repo := &ArticleRepo{MockAuditedRepository: baseRepo}
	// 使用抽象审计服务实现 + 扩展发布能力
	svc := NewArticleService(repo)
	router := mocks.NewMockRouter()
	// 使用审计路由构建器注册标准 + 审计扩展接口
	_ = api.NewAuditedApiBuilder(svc, mocks.NewNoopValidator()).
		Route(func(rc *api.RouteConfig) { rc.BasePath = "/api/articles" }).
		Build(router)
	// 扩展业务方法：发布文章（示例）
	router.POST("/api/articles/:id/publish", func(ctx httpx.IHttpContext) error { return svc.Publish(context.Background(), 1, "system") })
	router.PrintRoutes()

	// 此示例仅演示路由注册，业务调用留给实际系统验证
}
