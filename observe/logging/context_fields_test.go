package logging

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"

	"gochen/contextx/field"
)

func TestLogPreservesCallerFields(t *testing.T) {
	ctx, err := field.WithTenantID(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, logger := range []ILogger{
		NewStdLoggerWithWriter("", io.Discard),
		NewLogger(Config{Format: JSONFormat, Writer: io.Discard}),
	} {
		backing := []Field{String("request", "one"), String("reserved", "unchanged")}
		before := append([]Field(nil), backing...)
		logger.Info(ctx, "test", backing[:1]...)
		if !reflect.DeepEqual(before, backing) {
			t.Errorf("%T modified caller fields: got %v, want %v", logger, backing, before)
		}

		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				logger.Info(ctx, "concurrent", backing[:1]...)
			})
		}
		wg.Wait()
	}
}
