package rest

import (
	"net/http"

	"gochen/errors"
	"gochen/httpx"
)

func (rb *RouteBuilder[T, ID]) handleListDeleted(c httpx.IContext) error {
	if !rb.auditedEnabled {
		return errors.NewCode(errors.NotFound, "route not enabled")
	}
	opts, err := rb.parsePaginationOptions(c)
	if err != nil {
		return err
	}
	data, err := rb.auditedService.ListDeleted(c.RequestContext(), opts.Offset(), opts.Size)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, httpx.JSONValue(rb.config.Response.ResponseWrapper(data)))
}

func (rb *RouteBuilder[T, ID]) handleRestore(c httpx.IContext) error {
	if !rb.auditedEnabled {
		return errors.NewCode(errors.NotFound, "route not enabled")
	}
	id, err := rb.parseID(c)
	if err != nil {
		return err
	}

	ctx, operator, err := rb.mustAuditedContext(c)
	if err != nil {
		return err
	}
	if err := rb.auditedService.Restore(ctx, id, operator); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, httpx.JSONValue(rb.config.Response.ResponseWrapper(nil)))
}

func (rb *RouteBuilder[T, ID]) handlePurge(c httpx.IContext) error {
	if !rb.auditedEnabled {
		return errors.NewCode(errors.NotFound, "route not enabled")
	}
	id, err := rb.parseID(c)
	if err != nil {
		return err
	}

	ctx, _, err := rb.mustAuditedContext(c)
	if err != nil {
		return err
	}
	if err := rb.auditedService.Purge(ctx, id); err != nil {
		return err
	}

	wrappedData := rb.config.Response.ResponseWrapper(nil)
	return c.JSON(http.StatusOK, httpx.JSONValue(wrappedData))
}
