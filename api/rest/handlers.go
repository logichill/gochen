package rest

import (
	"net/http"
	"strings"

	auth "gochen/auth"
	"gochen/errors"
	"gochen/httpx"
)

func (rb *RouteBuilder[T, ID]) handleList(c httpx.IContext) error {
	if rb.config.Query.EnablePagination {
		return rb.handlePagedList(c)
	}

	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	if ctx, _, err = rb.authorize(c, ctx, rb.listPermission()); err != nil {
		return err
	}

	query, err := rb.parseQueryParams(c)
	if err != nil {
		return err
	}
	listSvc, ok := rb.listService()
	if !ok {
		return errors.NewCode(errors.InvalidInput, "list route requires service to implement ListByQuery")
	}
	result, err := listSvc.ListByQuery(ctx, query)
	if err != nil {
		return err
	}

	wrappedData := rb.config.Response.ResponseWrapper(result)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

func (rb *RouteBuilder[T, ID]) handlePagedList(c httpx.IContext) error {
	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	if ctx, _, err = rb.authorize(c, ctx, rb.listPermission()); err != nil {
		return err
	}

	options, err := rb.parsePaginationOptions(c)
	if err != nil {
		return err
	}
	listSvc, ok := rb.pagedListService()
	if !ok {
		return errors.NewCode(errors.InvalidInput, "list route with pagination requires service to implement ListPage")
	}
	result, err := listSvc.ListPage(ctx, options.ToPageRequest())
	if err != nil {
		return err
	}

	wrappedData := rb.config.Response.ResponseWrapper(result)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

func (rb *RouteBuilder[T, ID]) handleGet(c httpx.IContext) error {
	ctx, err := rb.serviceContext(c)
	if err != nil {
		return err
	}
	id, err := rb.parseID(c)
	if err != nil {
		return err
	}
	if permission := rb.getPermission(); permission != "" {
		scopedCtx, resource, ok, err := rb.contextForResourceID(ctx, id)
		if err != nil {
			return err
		}
		if ok {
			ctx = rb.syncRequestContext(c, scopedCtx)
			if ctx, _, err = rb.authorize(c, ctx, permission, auth.AuthResourceFromBoundary(resource)); err != nil {
				return err
			}
			getSvc, ok := rb.getService()
			if !ok {
				return errors.NewCode(errors.InvalidInput, "get route requires service to implement Get")
			}
			entity, err := getSvc.Get(ctx, id)
			if err != nil {
				return err
			}

			wrappedData := rb.config.Response.ResponseWrapper(entity)
			return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
		}
	}

	getSvc, ok := rb.getService()
	if !ok {
		return errors.NewCode(errors.InvalidInput, "get route requires service to implement Get")
	}
	entity, err := getSvc.Get(ctx, id)
	if err != nil {
		return err
	}
	if ctx, _, err = rb.authorize(c, ctx, rb.getPermission(), entity); err != nil {
		return err
	}

	wrappedData := rb.config.Response.ResponseWrapper(entity)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

func (rb *RouteBuilder[T, ID]) handleCreate(c httpx.IContext) error {
	var entity T
	if err := c.BindJSON(&entity); err != nil {
		if errors.Is(err, errors.PayloadTooLarge) {
			return err
		}
		return errors.Wrap(err, errors.InvalidInput, "invalid request data")
	}

	// 执行自定义验证
	if rb.config.Body.Validator != nil {
		if err := rb.config.Body.Validator(entity); err != nil {
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
	if permission := rb.createPermission(); permission != "" {
		ctx, decision, err := rb.authorize(c, ctx, permission, entity)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.writeConstraintWriter()
		if err := writer.CreateWithConstraint(ctx, entity, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else {
		createSvc, ok := rb.createService()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "create route requires service to implement Create")
		}
		if err := createSvc.Create(ctx, entity); err != nil {
			return err
		}
	}

	result := map[string]any{
		"id": entity.GetID(),
	}
	wrappedData := rb.config.Response.ResponseWrapper(result)

	// 根据配置选择返回 201 或 200
	statusCode := http.StatusOK
	if rb.config.Response.UseHTTP201ForCreate {
		statusCode = http.StatusCreated
	}
	return c.JSON(statusCode, httpx.JSONValue(wrappedData))
}

// handleUpdate 更新实体；audited 场景要求显式 version，并禁止通过更新篡改审计/软删字段。
func (rb *RouteBuilder[T, ID]) handleUpdate(c httpx.IContext) error {
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

	id, err := rb.parseID(c)
	if err != nil {
		return err
	}
	if permission := rb.updatePermission(); permission != "" {
		scopedCtx, _, ok, err := rb.contextForResourceID(ctx, id)
		if err != nil {
			return err
		}
		if ok {
			ctx = rb.syncRequestContext(c, scopedCtx)
		}
	}

	// 首先获取现有实体
	getSvc, ok := rb.getService()
	if !ok {
		return errors.NewCode(errors.InvalidInput, "update route requires service to implement Get")
	}
	entity, err := getSvc.Get(ctx, id)
	if err != nil {
		return err
	}

	// audited 更新：显式要求 version，并禁止通过更新接口篡改审计/软删字段。
	if rb.auditedEnabled {
		bodyObj, err := parseJSONBodyObject(c)
		if err != nil {
			return err
		}
		if err := rejectAuditedUpdateForbiddenFields(bodyObj); err != nil {
			return err
		}
		wantVer, err := requireUint64JSONField(bodyObj, "version")
		if err != nil {
			return err
		}
		if got := entity.GetVersion(); wantVer != got {
			return errors.NewCode(errors.Concurrency, "concurrency conflict").
				WithContext("aggregate_id", id).
				WithContext("expected_version", wantVer).
				WithContext("actual_version", got)
		}
	}

	// 绑定请求数据到现有实体
	if err := bindJSONIntoEntity(c, &entity); err != nil {
		if errors.Is(err, errors.PayloadTooLarge) {
			return err
		}
		return errors.Wrap(err, errors.InvalidInput, "invalid request data")
	}
	if err := ensureEntityIDMatchesPath(&entity, id); err != nil {
		return err
	}

	// 执行自定义验证
	if rb.config.Body.Validator != nil {
		if err := rb.config.Body.Validator(entity); err != nil {
			return err
		}
	}

	if permission := rb.updatePermission(); permission != "" {
		ctx, decision, err := rb.authorize(c, ctx, permission, entity)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.writeConstraintWriter()
		if err := writer.UpdateWithConstraint(ctx, entity, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else {
		updateSvc, ok := rb.updateService()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "update route requires service to implement Update")
		}
		if err := updateSvc.Update(ctx, entity); err != nil {
			return err
		}
	}

	wrappedData := rb.config.Response.ResponseWrapper(entity)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

// handleDelete 删除实体；audited 场景走软删，并显式禁止用 query param 触发 purge。
func (rb *RouteBuilder[T, ID]) handleDelete(c httpx.IContext) error {
	// 明确不支持通过 query param 触发 purge，避免语义歧义与误用。
	if strings.TrimSpace(c.Query("purge")) != "" {
		return errors.NewCode(errors.InvalidInput, "purge query param is not supported; use DELETE /:id/purge")
	}

	id, err := rb.parseID(c)
	if err != nil {
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

	if permission := rb.deletePermission(); permission != "" {
		scopedCtx, resource, ok, err := rb.contextForResourceID(ctx, id)
		if err != nil {
			return err
		}
		if ok {
			ctx = rb.syncRequestContext(c, scopedCtx)
			ctx, decision, err := rb.authorize(c, ctx, permission, auth.AuthResourceFromBoundary(resource))
			if err != nil {
				return err
			}
			ctx = auth.BindConstraintMetadata(ctx, decision)
			writer, _ := rb.writeConstraintWriter()
			if err := writer.DeleteWithConstraint(ctx, id, auth.WriteConstraintFromDecision(decision)); err != nil {
				return err
			}
			wrappedData := rb.config.Response.ResponseWrapper(nil)
			return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
		}
		getSvc, ok := rb.getService()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "authz-enabled delete route requires service to implement Get")
		}
		entity, err := getSvc.Get(ctx, id)
		if err != nil {
			return err
		}
		ctx, decision, err := rb.authorize(c, ctx, permission, entity)
		if err != nil {
			return err
		}
		ctx = auth.BindConstraintMetadata(ctx, decision)
		writer, _ := rb.writeConstraintWriter()
		if err := writer.DeleteWithConstraint(ctx, id, auth.WriteConstraintFromDecision(decision)); err != nil {
			return err
		}
	} else if rb.auditedEnabled {
		if err := rb.auditedService.Delete(ctx, id); err != nil {
			return err
		}
	} else {
		deleteSvc, ok := rb.deleteService()
		if !ok {
			return errors.NewCode(errors.InvalidInput, "delete route requires service to implement Delete")
		}
		if err := deleteSvc.Delete(ctx, id); err != nil {
			return err
		}
	}

	wrappedData := rb.config.Response.ResponseWrapper(nil)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}

// handleAuditTrail 查询 audited 实体的审计轨迹。
func (rb *RouteBuilder[T, ID]) handleAuditTrail(c httpx.IContext) error {
	if !rb.auditedEnabled {
		return errors.NewCode(errors.NotFound, "route not enabled")
	}
	id, err := rb.parseID(c)
	if err != nil {
		return err
	}

	// 复用通用分页解析，然后计算 offset/limit
	opts, err := rb.parsePaginationOptions(c)
	if err != nil {
		return err
	}
	recs, err := rb.auditedService.AuditTrail(c.RequestContext(), id, opts.Offset(), opts.Size)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, httpx.JSONValue(rb.config.Response.ResponseWrapper(recs)))
}
