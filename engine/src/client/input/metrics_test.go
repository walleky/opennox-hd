package input

import (
	"testing"
	"time"
)

func TestInputMetricsAreOptInAndReportQueuePressure(t *testing.T) {
	m := &inputMetrics{}
	m.observeDrop(true)
	m.observeAge(true, time.Now().Add(-time.Millisecond).UnixNano())
	if got := m.snapshot(); got != (InputMetrics{}) {
		t.Fatalf("disabled metrics = %#v", got)
	}

	m.enabled.Store(true)
	m.observeDrop(true)
	m.observeDrop(false)
	m.observeAge(true, time.Now().Add(-time.Millisecond).UnixNano())
	m.observeAge(false, time.Now().Add(-time.Millisecond).UnixNano())
	got := m.snapshot()
	if got.MouseDrops != 1 || got.KeyDrops != 1 || got.MouseAgeCount != 1 || got.KeyAgeCount != 1 || got.MouseAgeAverage <= 0 || got.KeyAgeMax <= 0 {
		t.Fatalf("metrics = %#v", got)
	}
}
