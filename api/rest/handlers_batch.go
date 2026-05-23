package rest

import (
	"encoding/json"
	"net/http"
	"reflect"

	auth "gochen/auth"
	"gochen/errors"
	"gochen/httpx"
)

func (rb *RouteBuilder[T, ID]) handleCreateAll(c httpx.IContext) error {
	if err := rb.rejectBatchAuthorization(); err != nil {
		return err
	}
	var entities []T
	if err := c.BindJSON(&entities); err != nil {
		if errors.Is(err, errors.PayloadTooLarge) {
			return err
		}
		return errors.Wrap(err, errors.InvalidInput, "invalid request data")
	}

	// P1：拒绝 nil 元素
	if err := rejectNilEntities(entities); err != nil {
		return err
	}

	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	if rb.auditedEnabled {
		auditCtx, _, err := rb.mustAuditedContext(c)
		if err != nil {
			return err
		}
		ctx = auditCtx
	}
	if permission := rb.batchCreatePermission(); permission != "" {
		ctx, decision, err := rb.authorize(c, ctx, permission, batchTargets(entities)...)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.batchWriteConstraintWriter()
		if err := writer.CreateAllWithConstraint(ctx, entities, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else {
		writer, ok := rb.batchWriter()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "batch routes require a built-in batch writer or service-specific batch writer")
		}
		if err := writer.CreateAll(ctx, entities); err != nil {
			return err
		}
	}

	// 收集创建的 ID 列表
	ids := make([]ID, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, entity.GetID())
	}

	result := map[string]any{
		"count": len(entities),
		"ids":   ids,
	}
	wrappedData := rb.config.Response.ResponseWrapper(result)

	// 根据配置选择返回 201 或 200
	statusCode := http.StatusCreated
	if !rb.config.Response.UseHTTP201ForCreate {
		statusCode = http.StatusOK
	}
	return c.JSON(statusCode, httpx.JSONValue(wrappedData))
}

func (rb *RouteBuilder[T, ID]) handleUpdateBatch(c httpx.IContext) error {
	if err := rb.rejectBatchAuthorization(); err != nil {
		return err
	}
	// 先获取原始 body 用于 audited 场景的校验
	body, err := c.Body()
	if err != nil {
		if errors.Is(err, errors.PayloadTooLarge) {
			return err
		}
		return errors.Wrap(err, errors.InvalidInput, "failed to read request body")
	}

	var entities []T
	if err := json.Unmarshal(body, &entities); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "invalid request data")
	}

	// P1：拒绝 nil 元素
	if err := rejectNilEntities(entities); err != nil {
		return err
	}

	// P0：audited 场景的字段/版本约束校验
	if rb.auditedEnabled {
		if err := validateAuditedBatchUpdate(body, entities); err != nil {
			return err
		}
	}

	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	if rb.auditedEnabled {
		auditCtx, _, err := rb.mustAuditedContext(c)
		if err != nil {
			return err
		}
		ctx = auditCtx
	}
	if permission := rb.batchUpdatePermission(); permission != "" {
		ctx, decision, err := rb.authorize(c, ctx, permission, batchTargets(entities)...)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.batchWriteConstraintWriter()
		if err := writer.UpdateAllWithConstraint(ctx, entities, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else {
		writer, ok := rb.batchWriter()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "batch routes require a built-in batch writer or service-specific batch writer")
		}
		if err := writer.UpdateAll(ctx, entities); err != nil {
			return err
		}
	}

	result := map[string]any{
		"count": len(entities),
	}
	wrappedData := rb.config.Response.ResponseWrapper(result)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

func (rb *RouteBuilder[T, ID]) handleDeleteBatch(c httpx.IContext) error {
	if err := rb.rejectBatchAuthorization(); err != nil {
		return err
	}
	var req struct {
		IDs []ID `json:"ids" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		if errors.Is(err, errors.PayloadTooLarge) {
			return err
		}
		return errors.Wrap(err, errors.InvalidInput, "invalid request data")
	}

	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	if rb.auditedEnabled {
		auditCtx, _, err := rb.mustAuditedContext(c)
		if err != nil {
			return err
		}
		ctx = auditCtx
	}
	if permission := rb.batchDeletePermission(); permission != "" {
		targets := make([]any, 0, len(req.IDs))
		if _, ok := rb.resourceBoundaryRepository(); ok {
			for _, id := range req.IDs {
				resource, _, err := rb.resolveResourceBoundary(ctx, id)
				if err != nil {
					return err
				}
				targets = append(targets, auth.AuthResourceFromBoundary(resource))
			}
		} else {
			entities := make([]T, 0, len(req.IDs))
			for _, id := range req.IDs {
				getSvc, ok := rb.getService()
				if !ok {
					return errors.NewCode(errors.InvalidInput, "authz-enabled batch delete route requires service to implement Get")
				}
				entity, err := getSvc.Get(ctx, id)
				if err != nil {
					return err
				}
				entities = append(entities, entity)
			}
			targets = batchTargets(entities)
		}
		ctx, decision, err := rb.authorize(c, ctx, permission, targets...)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.batchWriteConstraintWriter()
		if err := writer.DeleteAllWithConstraint(ctx, req.IDs, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else {
		writer, ok := rb.batchWriter()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "batch routes require a built-in batch writer or service-specific batch writer")
		}
		if err := writer.DeleteAll(ctx, req.IDs); err != nil {
			return err
		}
	}

	result := map[string]any{
		"count": len(req.IDs),
	}
	wrappedData := rb.config.Response.ResponseWrapper(result)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

func rejectNilEntities[T any](entities []T) error {
	for i, entity := range entities {
		v := reflect.ValueOf(entity)
		if v.Kind() == reflect.Ptr && v.IsNil() {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").WithContext("index", i)
		}
	}
	return nil
}

// validateAuditedBatchUpdate 校验Audited批量Update。
func validateAuditedBatchUpdate[T any](body []byte, entities []T) error {
	// 解析为 JSON 数组以便逐项校验
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "invalid JSON array")
	}

	if len(items) != len(entities) {
		return errors.NewCode(errors.InvalidInput, "entity count mismatch")
	}

	for i, item := range items {
		// 校验必须携带 version
		if _, err := requireUint64JSONField(item, "version"); err != nil {
			return errors.Wrap(err, errors.InvalidInput, "batch update validation failed").
				WithContext("index", i)
		}

		// 校验禁止更新托管字段
		if err := rejectAuditedUpdateForbiddenFields(item); err != nil {
			return errors.Wrap(err, errors.InvalidInput, "batch update validation failed").
				WithContext("index", i)
		}
	}

	return nil
}

func batchTargets[T any](values []T) []any {
	if len(values) == 0 {
		return nil
	}
	targets := make([]any, 0, len(values))
	for _, value := range values {
		targets = append(targets, value)
	}
	return targets
}
