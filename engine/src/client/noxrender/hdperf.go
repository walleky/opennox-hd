package noxrender

import (
	"fmt"
	"image"
	"sort"
	"strings"
	"time"
)

// TextureDensityMetrics is a read-only snapshot of the optional 2x replay
// monitor. Counters are cumulative; frame timings use a bounded recent window.
type TextureDensityMetrics struct {
	Frames, DecodeMisses, DecodeFailures, CacheEvictions, SurfaceRecreations uint64
	CacheBytes, CacheHighWater                                               int64
	QueueCount, QueueBytes, QueueRejected, FontFallbacks                     uint64
	FrameAverage, FrameP95, FrameMax                                         time.Duration
	DecodeTime, ExpansionTime, ReplayTime, UploadTime                        time.Duration
	QueueRejects                                                             map[string]uint64
}

type textureDensityPerf struct {
	enabled                                           bool
	frames                                            []time.Duration
	frameAt                                           int
	frameCount                                        uint64
	decodeMisses, decodeFailures, cacheEvictions      uint64
	decodeTime, expansionTime, replayTime, uploadTime time.Duration
	cacheBytes, cacheHighWater                        int64
	queueRejected, fontFallbacks                      uint64
	queueRejects                                      [textureDensityRejectCount]uint64
	trace                                             bool
	traceAccepted, traceRejected, traceOwnership      uint64
	traceRecords                                      []textureDensityTraceRecord
	traceCurrent                                      int
}

type textureDensityTraceRecord struct {
	Text                bool
	Bounds              image.Rectangle
	Visible, Suppressed uint64
}

func newTextureDensityPerf() *textureDensityPerf {
	return &textureDensityPerf{frames: make([]time.Duration, 120)}
}

// SetTextureDensityMetricsEnabled gates timers. It is intentionally opt-in so
// diagnostics-off rendering does not call time.Now in hot paths.
func (r *NoxRender) SetTextureDensityMetricsEnabled(enabled bool) {
	if r != nil && r.hdPerf != nil {
		r.hdPerf.enabled = enabled
	}
}

func (r *NoxRender) TextureDensityMetrics() TextureDensityMetrics {
	if r == nil || r.hdPerf == nil {
		return TextureDensityMetrics{}
	}
	p := r.hdPerf
	m := TextureDensityMetrics{
		Frames: p.frameCount, DecodeMisses: p.decodeMisses, DecodeFailures: p.decodeFailures,
		CacheEvictions: p.cacheEvictions, CacheBytes: p.cacheBytes, CacheHighWater: p.cacheHighWater,
		QueueRejected: p.queueRejected, FontFallbacks: p.fontFallbacks,
		DecodeTime: p.decodeTime, ExpansionTime: p.expansionTime, ReplayTime: p.replayTime, UploadTime: p.uploadTime,
		QueueCount: uint64(len(r.textureDensity)), QueueBytes: uint64(r.textureDensityBytes), QueueRejects: make(map[string]uint64),
	}
	for i, n := range p.queueRejects {
		if n != 0 {
			m.QueueRejects[textureDensityRejectName(i)] = n
		}
	}
	if p.frameCount != 0 {
		vals := make([]time.Duration, 0, len(p.frames))
		for _, v := range p.frames {
			if v > 0 {
				vals = append(vals, v)
			}
		}
		if len(vals) > 0 {
			sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
			var total time.Duration
			for _, v := range vals {
				total += v
			}
			m.FrameAverage = total / time.Duration(len(vals))
			m.FrameP95 = vals[(len(vals)*95+99)/100-1]
			m.FrameMax = vals[len(vals)-1]
		}
	}
	return m
}

func (r *NoxRender) ArmTextureDensityTrace() {
	if r == nil || r.hdPerf == nil {
		return
	}
	r.hdPerf.trace = true
	r.hdPerf.traceAccepted = 0
	r.hdPerf.traceRejected = 0
	r.hdPerf.traceOwnership = 0
	r.hdPerf.traceRecords = r.hdPerf.traceRecords[:0]
	r.hdPerf.traceCurrent = -1
}

func (p *textureDensityPerf) beginTraceRecord(text bool, bounds image.Rectangle) int {
	if p == nil || !p.trace {
		return -1
	}
	p.traceRecords = append(p.traceRecords, textureDensityTraceRecord{Text: text, Bounds: bounds})
	return len(p.traceRecords) - 1
}

func (p *textureDensityPerf) traceResult(visible bool) {
	if p == nil || !p.trace || p.traceCurrent < 0 || p.traceCurrent >= len(p.traceRecords) {
		return
	}
	if visible {
		p.traceRecords[p.traceCurrent].Visible++
	} else {
		p.traceRecords[p.traceCurrent].Suppressed++
	}
}

func (r *NoxRender) emitTextureDensityTrace() {
	if r == nil || r.hdPerf == nil || !r.hdPerf.trace {
		return
	}
	p := r.hdPerf
	var records strings.Builder
	for i, rec := range p.traceRecords {
		if i != 0 {
			records.WriteByte(',')
		}
		kind := "s"
		if rec.Text {
			kind = "t"
		}
		fmt.Fprintf(&records, "%d%s(%d,%d,%d,%d)=%d/%d", i, kind, rec.Bounds.Min.X, rec.Bounds.Min.Y, rec.Bounds.Max.X, rec.Bounds.Max.Y, rec.Visible, rec.Suppressed)
	}
	Log.Printf("[hdtrace] accepted=%d rejected=%d ownership=%d records=%s\n", p.traceAccepted, p.traceRejected, p.traceOwnership, records.String())
	p.trace = false
	p.traceCurrent = -1
}

func (p *textureDensityPerf) recordFrame(d time.Duration) {
	if p == nil {
		return
	}
	p.frames[p.frameAt] = d
	p.frameAt = (p.frameAt + 1) % len(p.frames)
	p.frameCount++
}

func (p *textureDensityPerf) reject(reason int) {
	if p == nil || reason < 0 || reason >= len(p.queueRejects) {
		return
	}
	p.queueRejected++
	p.queueRejects[reason]++
	if p.trace {
		p.traceRejected++
	}
}

const (
	textureDensityRejectCapacity = iota
	textureDensityRejectInvalid
	textureDensityRejectMissingGlyph
	textureDensityRejectCompound
	textureDensityRejectCount
)

func textureDensityRejectName(reason int) string {
	switch reason {
	case textureDensityRejectCapacity:
		return "capacity"
	case textureDensityRejectInvalid:
		return "invalid"
	case textureDensityRejectMissingGlyph:
		return "missing_glyph"
	case textureDensityRejectCompound:
		return "compound_operation"
	default:
		return "unknown"
	}
}
