package crud

import (
	"context"
	"testing"

	"gochen/errors"
)

// nilPointerEntity 是指针类型实体：typed nil 场景的复现载体。
type nilPointerEntity struct {
	id int64
}

func (e *nilPointerEntity) GetID() int64       { return e.id }
func (e *nilPointerEntity) SetID(id int64)     { e.id = id }
func (e *nilPointerEntity) GetVersion() uint64 { return 0 }

type nilPointerRepo struct {
	created []*nilPointerEntity
}

func (r *nilPointerRepo) Create(_ context.Context, e *nilPointerEntity) error {
	r.created = append(r.created, e)
	return nil
}

func (r *nilPointerRepo) Update(_ context.Context, e *nilPointerEntity) error {
	r.created = append(r.created, e)
	return nil
}

func (r *nilPointerRepo) Delete(context.Context, int64) error { return nil }

func (r *nilPointerRepo) Get(context.Context, int64) (*nilPointerEntity, error) {
	return nil, errors.NewCode(errors.NotFound, "not found")
}

// P2-067 回归：typed nil 指针实体必须 fail-fast 返回 InvalidInput，而不是 panic。
func TestApplicationWriteRejectsTypedNilEntity(t *testing.T) {
	app, err := NewApplication[*nilPointerEntity, int64](&nilPointerRepo{}, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var nilEntity *nilPointerEntity
	if err := app.Create(context.Background(), nilEntity); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Create(typed nil) = %v, want InvalidInput", err)
	}
	if err := app.Update(context.Background(), nilEntity); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Update(typed nil) = %v, want InvalidInput", err)
	}
}

func TestApplicationBatchWriteRejectsTypedNilEntity(t *testing.T) {
	app, err := NewApplication[*nilPointerEntity, int64](&nilPointerRepo{}, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var nilEntity *nilPointerEntity
	if err := app.CreateAll(context.Background(), []*nilPointerEntity{nilEntity}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("CreateAll(typed nil) = %v, want InvalidInput", err)
	}
	if err := app.UpdateAll(context.Background(), []*nilPointerEntity{nilEntity}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("UpdateAll(typed nil) = %v, want InvalidInput", err)
	}
}
