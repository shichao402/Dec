package audit

import (
	"testing"
	"time"
)

func TestDbgQuota(t *testing.T) {
	agg := NewAccessAggregator()
	at := time.Date(2026, 10, 24, 13, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		d := agg.Offer(Entry{At: at.Add(time.Duration(i) * time.Millisecond), Event: EventSync, DeviceID: "dev-a", Source: "agent"})
		if i < 3 || i > 18 {
			t.Logf("i=%d emit=%v buckets=%d", i, d.Emit, len(agg.buckets))
		}
	}
	for k, b := range agg.buckets {
		t.Logf("bucket %q emitted=%d suppressed=%d", k, b.emitted, b.suppressed)
	}
}
