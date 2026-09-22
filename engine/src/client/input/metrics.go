package input

import (
	"sync/atomic"
	"time"
)

// InputMetrics is a concurrency-safe snapshot of input queue pressure. Age
// timing is only sampled while explicitly enabled by the perf monitor.
type InputMetrics struct {
	MouseDrops, KeyDrops           uint64
	MouseAgeCount, KeyAgeCount     uint64
	MouseAgeAverage, KeyAgeAverage time.Duration
	MouseAgeMax, KeyAgeMax         time.Duration
}

type inputMetrics struct {
	enabled                    atomic.Bool
	mouseDrops, keyDrops       atomic.Uint64
	mouseAgeCount, keyAgeCount atomic.Uint64
	mouseAgeTotal, keyAgeTotal atomic.Int64
	mouseAgeMax, keyAgeMax     atomic.Int64
}

func (m *inputMetrics) observeDrop(mouse bool) {
	if m == nil || !m.enabled.Load() {
		return
	}
	if mouse {
		m.mouseDrops.Add(1)
	} else {
		m.keyDrops.Add(1)
	}
}

func (m *inputMetrics) observeAge(mouse bool, stamp int64) {
	if m == nil || !m.enabled.Load() || stamp == 0 {
		return
	}
	ns := time.Now().UnixNano() - stamp
	if ns < 0 {
		ns = 0
	}
	if mouse {
		m.mouseAgeCount.Add(1)
		m.mouseAgeTotal.Add(ns)
		for {
			old := m.mouseAgeMax.Load()
			if ns <= old || m.mouseAgeMax.CompareAndSwap(old, ns) {
				break
			}
		}
	} else {
		m.keyAgeCount.Add(1)
		m.keyAgeTotal.Add(ns)
		for {
			old := m.keyAgeMax.Load()
			if ns <= old || m.keyAgeMax.CompareAndSwap(old, ns) {
				break
			}
		}
	}
}

func (m *inputMetrics) snapshot() InputMetrics {
	if m == nil {
		return InputMetrics{}
	}
	mm, km := m.mouseAgeCount.Load(), m.keyAgeCount.Load()
	out := InputMetrics{MouseDrops: m.mouseDrops.Load(), KeyDrops: m.keyDrops.Load(), MouseAgeCount: mm, KeyAgeCount: km, MouseAgeMax: time.Duration(m.mouseAgeMax.Load()), KeyAgeMax: time.Duration(m.keyAgeMax.Load())}
	if mm != 0 {
		out.MouseAgeAverage = time.Duration(m.mouseAgeTotal.Load() / int64(mm))
	}
	if km != 0 {
		out.KeyAgeAverage = time.Duration(m.keyAgeTotal.Load() / int64(km))
	}
	return out
}

func (h *Handler) SetMetricsEnabled(enabled bool) {
	if h != nil && h.metrics != nil {
		h.metrics.enabled.Store(enabled)
	}
}

func (h *Handler) InputMetrics() InputMetrics {
	if h == nil {
		return InputMetrics{}
	}
	return h.metrics.snapshot()
}
