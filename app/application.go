// Package service 提供应用服务层实现
package app

import (
	"context"
	"fmt"

	"gochen/domain/entity"
	repo "gochen/domain/repository"
	"gochen/domain/service"
	validation "gochen/validation"
)

// IApplication 应用服务接口
// 扩展基础 CRUD 服务，提供业务逻辑封装和生命周期管理
type IApplication[T entity.IEntity[int64]] interface {
	service.IService[T, int64]

	// 查询扩展
	ListByQuery(ctx context.Context, query *QueryParams) ([]T, error)
	ListPage(ctx context.Context, options *PaginationOptions) (*PagedResult[T], error)
	CountByQuery(ctx context.Context, query *QueryParams) (int64, error)

	// 批量操作
	CreateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error)
	UpdateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error)
	DeleteBatch(ctx context.Context, ids []int64) (*BatchOperationResult, error)

	// 业务操作钩子
	Validate(entity T) error
	BeforeCreate(ctx context.Context, entity T) error
	AfterCreate(ctx context.Context, entity T) error
	BeforeUpdate(ctx context.Context, entity T) error
	AfterUpdate(ctx context.Context, entity T) error
	BeforeDelete(ctx context.Context, id int64) error
	AfterDelete(ctx context.Context, id int64) error

	// 服务配置
	GetConfig() *ServiceConfig
}

// QueryParams 查询参数
type QueryParams struct {
	Filters map[string]string `json:"filters"`
	Sorts   map[string]string `json:"sorts"`
	Fields  []string          `json:"fields"`
}

// PaginationOptions 分页选项
type PaginationOptions struct {
	Page    int               `json:"page"`
	Size    int               `json:"size"`
	Sorts   map[string]string `json:"sorts"`
	Filters map[string]string `json:"filters"`
	Fields  []string          `json:"fields"`
}

// Validate 调整并验证分页参数
func (p *PaginationOptions) Validate(maxSize int) error {
	if p.Page < 1 {
		p.Page = 1
	}

	if p.Size < 1 {
		p.Size = 10
	}

	if maxSize > 0 && p.Size > maxSize {
		p.Size = maxSize
	}

	return nil
}

// PagedResult 分页结果
type PagedResult[T entity.IEntity[int64]] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// BatchOperationResult 批量操作结果
type BatchOperationResult struct {
	Total      int      `json:"total"`
	Success    int      `json:"success"`
	Failed     int      `json:"failed"`
	SuccessIDs []int64  `json:"success_ids,omitempty"`
	FailedIDs  []int64  `json:"failed_ids,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// ServiceConfig 服务配置
type ServiceConfig struct {
	// 自动验证
	AutoValidate bool

	// 自动时间戳
	AutoTimestamp bool

	// 软删除
	SoftDelete bool

	// 审计字段
	AuditFields bool

	// 最大批量操作数量
	MaxBatchSize int

	// 启用缓存
	EnableCache bool

	// 缓存过期时间（秒）
	CacheTTL int

	// 启用审计日志
	EnableAudit bool

	// 乐观锁
	OptimisticLock bool

	// 事务管理
	ITransactional bool
}

// DefaultServiceConfig 默认服务配置
func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		AutoValidate:   true,
		AutoTimestamp:  true,
		SoftDelete:     false,
		AuditFields:    false,
		MaxBatchSize:   1000,
		EnableCache:    false,
		CacheTTL:       300, // 5 minutes
		EnableAudit:    false,
		OptimisticLock: false,
		ITransactional: false,
	}
}

// Application 应用服务实现
type Application[T entity.IEntity[int64]] struct {
	*service.CRUDService[T, int64]
	validator validation.IValidator
	config    *ServiceConfig
}

// NewApplication 创建应用服务
func NewApplication[T entity.IEntity[int64]](
	repository repo.IRepository[T, int64],
	validator validation.IValidator,
	config *ServiceConfig,
) IApplication[T] {
	if config == nil {
		config = DefaultServiceConfig()
	}

	return &Application[T]{
		CRUDService: service.NewCRUDService[T, int64](repository),
		validator:   validator,
		config:      config,
	}
}

// GetConfig 获取服务配置
func (s *Application[T]) GetConfig() *ServiceConfig {
	return s.config
}

// UpdateConfig 更新服务配置
func (s *Application[T]) UpdateConfig(config *ServiceConfig) {
	if config == nil {
		return
	}
	copyCfg := *config
	s.config = &copyCfg
}

// SetValidator 设置验证器
func (s *Application[T]) SetValidator(v validation.IValidator) {
	s.validator = v
}

// ListByQuery 根据查询参数获取列表
func (s *Application[T]) ListByQuery(ctx context.Context, query *QueryParams) ([]T, error) {
	if query == nil {
		query = &QueryParams{}
	}

	// 构建查询选项
	options := &repo.QueryOptions{
		Filters: make(map[string]interface{}),
	}

	// 转换过滤条件
	for k, v := range query.Filters {
		options.Filters[k] = v
	}

	// 转换排序条件
	if len(query.Sorts) > 0 {
		for field, direction := range query.Sorts {
			options.OrderBy = field
			options.OrderDesc = direction == "desc"
			break // 暂时只支持单字段排序
		}
	}

	// 执行查询
	// 注意：这里假设仓储实现了 IQueryableRepository 接口
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		return queryableRepo.Query(ctx, *options)
	}

	// 如果不支持复杂查询，则使用基础查询
	return s.Repository().List(ctx, 0, 1000)
}

// ListPage 分页查询
func (s *Application[T]) ListPage(ctx context.Context, options *PaginationOptions) (*PagedResult[T], error) {
	if options == nil {
		options = &PaginationOptions{Page: 1, Size: 10}
	}

	// 验证分页参数
	if err := options.Validate(s.config.MaxBatchSize); err != nil {
		return nil, fmt.Errorf("invalid pagination options: %w", err)
	}

	// 构建查询选项
	queryOpts := &repo.QueryOptions{
		Offset:  (options.Page - 1) * options.Size,
		Limit:   options.Size,
		Filters: make(map[string]interface{}),
	}

	// 转换过滤条件
	for k, v := range options.Filters {
		queryOpts.Filters[k] = v
	}

	// 转换排序条件
	if len(options.Sorts) > 0 {
		for field, direction := range options.Sorts {
			queryOpts.OrderBy = field
			queryOpts.OrderDesc = direction == "desc"
			break // 暂时只支持单字段排序
		}
	}

	var data []T
	var total int64
	var err error

	// 尝试使用可查询仓储
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		data, err = queryableRepo.Query(ctx, *queryOpts)
		if err != nil {
			return nil, err
		}

		total, err = queryableRepo.QueryCount(ctx, *queryOpts)
		if err != nil {
			return nil, err
		}
	} else {
		// 使用基础查询
		data, err = s.Repository().List(ctx, queryOpts.Offset, queryOpts.Limit)
		if err != nil {
			return nil, err
		}

		total, err = s.Repository().Count(ctx)
		if err != nil {
			return nil, err
		}
	}

	// 计算总页数和导航信息
	totalPages := int((total + int64(options.Size) - 1) / int64(options.Size))
	hasNext := options.Page < totalPages
	hasPrev := options.Page > 1

	return &PagedResult[T]{
		Data:       data,
		Total:      total,
		Page:       options.Page,
		Size:       options.Size,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
	}, nil
}

// CountByQuery 根据查询参数统计数量
func (s *Application[T]) CountByQuery(ctx context.Context, query *QueryParams) (int64, error) {
	if query == nil || len(query.Filters) == 0 {
		return s.Repository().Count(ctx)
	}

	// 构建查询选项
	options := &repo.QueryOptions{
		Filters: make(map[string]interface{}),
	}

	// 转换过滤条件
	for k, v := range query.Filters {
		options.Filters[k] = v
	}

	// 尝试使用可查询仓储
	if queryableRepo, ok := s.Repository().(repo.IQueryableRepository[T, int64]); ok {
		return queryableRepo.QueryCount(ctx, *options)
	}

	// 如果不支持复杂查询，返回总数
	return s.Repository().Count(ctx)
}

// CreateBatch 批量创建
func (s *Application[T]) CreateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error) {
	if len(entities) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(entities) > s.config.MaxBatchSize {
		return nil, service.NewValidationError("batch size exceeds maximum limit of %d", s.config.MaxBatchSize)
	}

	result := &BatchOperationResult{
		Total:      len(entities),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(repo.IBatchOperations[T, int64]); ok {
		pending := make([]T, 0, len(entities))
		for _, entity := range entities {
			if err := s.BeforeCreate(ctx, entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			if err := s.Validate(entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			pending = append(pending, entity)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.CreateAll(ctx, pending); err != nil {
			return nil, err
		}

		for _, entity := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
			_ = s.AfterCreate(ctx, entity) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, entity := range entities {
		if err := s.Create(ctx, entity); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, entity.GetID())
			result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
		}
	}

	return result, nil
}

// UpdateBatch 批量更新
func (s *Application[T]) UpdateBatch(ctx context.Context, entities []T) (*BatchOperationResult, error) {
	if len(entities) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(entities) > s.config.MaxBatchSize {
		return nil, service.NewValidationError("batch size exceeds maximum limit of %d", s.config.MaxBatchSize)
	}

	result := &BatchOperationResult{
		Total:      len(entities),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(repo.IBatchOperations[T, int64]); ok {
		pending := make([]T, 0, len(entities))
		for _, entity := range entities {
			if err := s.BeforeUpdate(ctx, entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			if err := s.Validate(entity); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, entity.GetID())
				result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
				continue
			}
			pending = append(pending, entity)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.UpdateBatch(ctx, pending); err != nil {
			return nil, err
		}

		for _, entity := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
			_ = s.AfterUpdate(ctx, entity) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, entity := range entities {
		if err := s.Update(ctx, entity); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, entity.GetID())
			result.Errors = append(result.Errors, fmt.Sprintf("entity %v: %s", entity.GetID(), err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, entity.GetID())
		}
	}

	return result, nil
}

// DeleteBatch 批量删除
func (s *Application[T]) DeleteBatch(ctx context.Context, ids []int64) (*BatchOperationResult, error) {
	if len(ids) == 0 {
		return &BatchOperationResult{}, nil
	}

	if len(ids) > s.config.MaxBatchSize {
		return nil, service.NewValidationError("batch size exceeds maximum limit of %d", s.config.MaxBatchSize)
	}

	result := &BatchOperationResult{
		Total:      len(ids),
		SuccessIDs: make([]int64, 0),
		FailedIDs:  make([]int64, 0),
		Errors:     make([]string, 0),
	}

	if batchRepo, ok := s.Repository().(repo.IBatchOperations[T, int64]); ok {
		pending := make([]int64, 0, len(ids))
		for _, id := range ids {
			if err := s.BeforeDelete(ctx, id); err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
				continue
			}

			exists, err := s.Repository().Exists(ctx, id)
			if err != nil {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
				continue
			}

			if !exists {
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, id)
				result.Errors = append(result.Errors, fmt.Sprintf("entity %d: not found", id))
				continue
			}

			pending = append(pending, id)
		}

		if len(pending) == 0 {
			return result, nil
		}

		if err := batchRepo.DeleteBatch(ctx, pending); err != nil {
			return nil, err
		}

		for _, id := range pending {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, id)
			_ = s.AfterDelete(ctx, id) // 忽略后置钩子错误
		}

		return result, nil
	}

	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, id)
			result.Errors = append(result.Errors, fmt.Sprintf("entity %d: %s", id, err.Error()))
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, id)
		}
	}

	return result, nil
}

// Create 创建实体（带生命周期钩子）
func (s *Application[T]) Create(ctx context.Context, entity T) error {
	// 执行前置操作
	if err := s.BeforeCreate(ctx, entity); err != nil {
		return err
	}

	// 执行创建
	if err := s.CRUDService.Create(ctx, entity); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterCreate(ctx, entity)
}

// Update 更新实体（带生命周期钩子）
func (s *Application[T]) Update(ctx context.Context, entity T) error {
	// 执行前置操作
	if err := s.BeforeUpdate(ctx, entity); err != nil {
		return err
	}

	// 执行更新
	if err := s.CRUDService.Update(ctx, entity); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterUpdate(ctx, entity)
}

// Delete 删除实体（带生命周期钩子）
func (s *Application[T]) Delete(ctx context.Context, id int64) error {
	// 执行前置操作
	if err := s.BeforeDelete(ctx, id); err != nil {
		return err
	}

	// 执行删除
	if err := s.CRUDService.Delete(ctx, id); err != nil {
		return err
	}

	// 执行后置操作
	return s.AfterDelete(ctx, id)
}

// 生命周期钩子方法（可被具体实现覆盖）
func (s *Application[T]) Validate(entity T) error {
	if s.config.AutoValidate && s.validator != nil {
		return s.validator.Validate(entity)
	}
	return nil
}

func (s *Application[T]) BeforeCreate(ctx context.Context, entity T) error { return nil }
func (s *Application[T]) AfterCreate(ctx context.Context, entity T) error  { return nil }
func (s *Application[T]) BeforeUpdate(ctx context.Context, entity T) error { return nil }
func (s *Application[T]) AfterUpdate(ctx context.Context, entity T) error  { return nil }
func (s *Application[T]) BeforeDelete(ctx context.Context, id int64) error { return nil }
func (s *Application[T]) AfterDelete(ctx context.Context, id int64) error  { return nil }
