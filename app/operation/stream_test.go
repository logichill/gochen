package operation

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"gochen/errors"
)

func TestPrepareStreamHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	PrepareStreamHeaders(rec.Header())

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type: %s", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("unexpected cache control: %s", got)
	}
}

func TestStreamWritesSettledOperationEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	PrepareStreamHeaders(rec.Header())

	err := Stream(context.Background(), rec, rec, func(ctx context.Context) (*Result, error) {
		return &Result{
			Operation: Operation{
				ID:     "op_stream",
				Type:   "task.complete",
				Mode:   ModeTracked,
				Status: StatusSettled,
			},
		}, nil
	}, nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: operation") {
		t.Fatalf("expected operation event, got %q", body)
	}
	if !strings.Contains(body, "\"status\":\"settled\"") {
		t.Fatalf("expected settled payload, got %q", body)
	}
}

func TestStreamRejectsNilLoaderResult(t *testing.T) {
	rec := httptest.NewRecorder()

	err := Stream(context.Background(), rec, rec, func(context.Context) (*Result, error) {
		return nil, nil
	}, nil)
	if err == nil {
		t.Fatalf("expected nil loader result error")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}
