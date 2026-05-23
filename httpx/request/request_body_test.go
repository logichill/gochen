package request

import (
	"net/http/httptest"
	"strings"
	"testing"

	"gochen/errors"
	"gochen/httpx"
)

func TestBindJSON_StrictRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"alice","extra":true}`},
		{name: "trailing data", body: `{"name":"alice"} {"name":"bob"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			err := BindJSON(httptest.NewRecorder(), req, map[string]any{}, "body", &payload{})
			if !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("BindJSON error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestReadBody_CachesForRepeatedReads(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"alice"}`))
	values := map[string]any{}

	first, err := ReadBody(httptest.NewRecorder(), req, values, "body")
	if err != nil {
		t.Fatalf("first ReadBody error = %v", err)
	}
	req.Body = nil
	second, err := ReadBody(httptest.NewRecorder(), req, values, "body")
	if err != nil {
		t.Fatalf("second ReadBody error = %v", err)
	}
	if string(first) != `{"name":"alice"}` || string(second) != string(first) {
		t.Fatalf("cached body mismatch: first=%q second=%q", first, second)
	}
}

func TestReadBody_EnforcesMaxBytes(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("12345"))
	values := map[string]any{
		httpx.MaxBodySizeKey: httpx.ValueOf[int64](4),
	}

	_, err := ReadBody(httptest.NewRecorder(), req, values, "body")
	if !errors.Is(err, errors.PayloadTooLarge) {
		t.Fatalf("ReadBody error = %v, want PayloadTooLarge", err)
	}
}
