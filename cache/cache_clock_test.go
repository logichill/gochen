package cache

import (
	"testing"
	"time"

	"gochen/clock"
)

func TestCache_TTL_UsesInjectedClock(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())

	c := New[string, string](Config{
		Name:  "ttl_clock",
		TTL:   1 * time.Second,
		Clock: clk,
	})

	c.Set("k", "v")

	// 0.5s 后仍有效
	clk.Advance(500 * time.Millisecond)
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("expected hit at +500ms, ok=%v, v=%q", ok, v)
	}

	// 再过 1.1s（从最近一次访问起算）应过期
	clk.Advance(1100 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatalf("expected miss after TTL elapsed")
	}
}
