// Package contracttest 提供跨仓储实现共用的行为契约。
package contracttest

import (
	"context"
	"reflect"
	"testing"

	"gochen/app"
	"gochen/contextx"
	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

// Document 同时声明两个授权维度、可空值、mutable 分类与隔离。
type Document struct {
	audited.AuditedEntity[int64]
	Tenant     string  `json:"tenant" dim:"tenant"`
	Department int64   `json:"department" dim:"department"`
	Region     *string `json:"region" dim:"region"`
	Classified bool    `json:"classified" dim:"classification"`
	Name       string  `json:"name"`
}

// Repository 是本契约实际消费的仓储能力。
type Repository interface {
	crud.IRepository[*Document, int64]
	crud.ITransferRepository[*Document, int64]
	WithinTx(context.Context, func(context.Context) error) error
	app.IExecutionDescriptorProvider
}

// Contract 返回本测试的资源能力。
func Contract() app.ResourceContract {
	return app.ResourceContract{Isolation: meta.ByDimension("tenant"), Mutable: []meta.Dimension{"classification"}, Assignable: []meta.Dimension{"department", "region"}, Transferable: []meta.Dimension{"department", "region"}}
}

// RunDimensions 使用同一组断言验证内存和 SQL 的条件化写入。
func RunDimensions(t *testing.T, factory func(*testing.T) Repository) {
	t.Helper()
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = contextx.WithOperator(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	code := func(t *testing.T, err error, wanted errors.ErrorCode) {
		t.Helper()
		if !errors.Is(err, wanted) {
			t.Fatalf("error = %v, want %v", err, wanted)
		}
	}
	t.Run("conditions and protected nullable input", func(t *testing.T) {
		r := factory(t)
		entity := &Document{Name: "original", Department: 1}
		must(t, r.Create(ctx, entity, spec.All()))
		for _, condition := range []spec.Spec{
			spec.And(spec.Eq(meta.ByDimension("department"), "1"), spec.IsNull(meta.ByDimension("region"))),
			spec.Or(spec.Eq(meta.ByDimension("department"), 2), spec.Eq(meta.ByField("Department"), 1)),
		} {
			_, err := r.Get(ctx, entity.ID, condition)
			must(t, err)
		}
		for _, condition := range []spec.Spec{spec.Eq(meta.ByDimension("region"), "north"), spec.In(meta.ByDimension("region"), "north", "south")} {
			_, err := r.Get(ctx, entity.ID, condition)
			code(t, err, errors.NotFound)
		}
		for _, change := range []func(*Document){
			func(d *Document) { d.Department = 2 }, func(d *Document) { v := "north"; d.Region = &v }, func(d *Document) { d.Tenant = "tenant-b" },
		} {
			candidate, err := r.Get(ctx, entity.ID, spec.All())
			must(t, err)
			change(candidate)
			candidate.Name = "forged"
			code(t, r.Update(ctx, candidate, spec.All()), errors.Forbidden)
			stored, err := r.Get(ctx, entity.ID, spec.All())
			must(t, err)
			if stored.Name != "original" || stored.Department != 1 || stored.Region != nil {
				t.Fatalf("failed update changed storage: %+v", stored)
			}
		}
		candidate, err := r.Get(ctx, entity.ID, spec.All())
		must(t, err)
		candidate.Classified = true
		candidate.Name = "changed"
		must(t, r.Update(ctx, candidate, spec.All()))
		code(t, r.Update(ctx, entity, spec.All()), errors.Concurrency)
		_, err = r.Get(ctx, entity.ID, spec.Eq(meta.ByDimension("missing"), 1))
		if err == nil {
			t.Fatal("missing binding accepted")
		}
	})
	t.Run("transfer scope version and independent images", func(t *testing.T) {
		r := factory(t)
		entity := &Document{Name: "original", Department: 1}
		must(t, r.Create(ctx, entity, spec.All()))
		using := spec.Eq(meta.ByDimension("department"), 1)
		check := spec.And(spec.Eq(meta.ByDimension("department"), 2), spec.Eq(meta.ByDimension("region"), "north"))
		changes := []crud.DimensionChange{{Dimension: "department", Value: "2"}, {Dimension: "region", Value: "north"}}
		_, err := r.Transfer(ctx, entity.ID, entity.Version, changes, using, check)
		code(t, err, errors.FailedPrecondition)
		var result crud.TransferResult[*Document]
		must(t, r.WithinTx(ctx, func(txCtx context.Context) error {
			var err error
			result, err = r.Transfer(txCtx, entity.ID, entity.Version, changes, using, check)
			return err
		}))
		if result.Before.Department != 1 || result.Before.Region != nil || result.After.Department != 2 || result.After.Region == nil || *result.After.Region != "north" || result.After.Version != entity.Version+1 || len(result.Changes) != 2 {
			t.Fatalf("transfer images: %+v", result)
		}
		result.Before.Name = "mutated before"
		*result.After.Region = "mutated result"
		stored, err := r.Get(ctx, entity.ID, check)
		must(t, err)
		if stored.Name != "original" || *stored.Region != "north" {
			t.Fatalf("returned images alias storage: %+v", stored)
		}
		code(t, r.WithinTx(ctx, func(txCtx context.Context) error {
			_, err := r.Transfer(txCtx, entity.ID, entity.Version, changes, spec.All(), spec.All())
			return err
		}), errors.Concurrency)
		code(t, r.WithinTx(ctx, func(txCtx context.Context) error {
			_, err := r.Transfer(txCtx, entity.ID, stored.Version, changes, spec.All(), spec.All())
			return err
		}), errors.InvalidInput)
		for _, invalid := range [][]crud.DimensionChange{
			{{Dimension: "tenant", Value: "tenant-b"}}, {{Dimension: "classification", Value: true}},
			{{Dimension: "department", Value: 3}, {Dimension: "department", Value: 4}},
		} {
			code(t, r.WithinTx(ctx, func(txCtx context.Context) error {
				_, err := r.Transfer(txCtx, entity.ID, stored.Version, invalid, spec.All(), spec.All())
				return err
			}), errors.InvalidInput)
		}
		code(t, r.WithinTx(ctx, func(txCtx context.Context) error {
			_, err := r.Transfer(txCtx, entity.ID, stored.Version, []crud.DimensionChange{{Dimension: "department", Value: 3}}, spec.All(), check)
			return err
		}), errors.Forbidden)
	})
	t.Run("transfer rollback and round trip invalidates old version", func(t *testing.T) {
		r := factory(t)
		entity := &Document{Department: 1}
		must(t, r.Create(ctx, entity, spec.All()))
		marker := errors.NewCode(errors.Internal, "rollback marker")
		err := r.WithinTx(ctx, func(txCtx context.Context) error {
			_, err := r.Transfer(txCtx, entity.ID, entity.Version, []crud.DimensionChange{{Dimension: "department", Value: 2}}, spec.All(), spec.All())
			if err != nil {
				return err
			}
			return marker
		})
		if !reflect.DeepEqual(err, marker) {
			t.Fatalf("rollback: %v", err)
		}
		stored, err := r.Get(ctx, entity.ID, spec.All())
		must(t, err)
		if stored.Department != 1 || stored.Version != entity.Version {
			t.Fatalf("rollback left writes: %+v", stored)
		}
		for _, destination := range []int64{2, 1} {
			must(t, r.WithinTx(ctx, func(txCtx context.Context) error {
				result, err := r.Transfer(txCtx, entity.ID, stored.Version, []crud.DimensionChange{{Dimension: "department", Value: destination}}, spec.All(), spec.All())
				stored = result.After
				return err
			}))
		}
		code(t, r.WithinTx(ctx, func(txCtx context.Context) error {
			_, err := r.Transfer(txCtx, entity.ID, entity.Version, []crud.DimensionChange{{Dimension: "department", Value: 2}}, spec.All(), spec.All())
			return err
		}), errors.Concurrency)
	})
}
