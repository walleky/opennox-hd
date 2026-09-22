package noxrender

import (
	"image"
	"image/color"
	"testing"
	"time"
)

func TestTextureDensityPerfRejectReasonsAndPercentiles(t *testing.T) {
	r := NewRender(nil, nil)
	r.hdPerf.reject(textureDensityRejectCapacity)
	r.hdPerf.reject(textureDensityRejectMissingGlyph)
	r.hdPerf.recordFrame(time.Millisecond)
	r.hdPerf.recordFrame(3 * time.Millisecond)
	m := r.TextureDensityMetrics()
	if m.QueueRejected != 2 || m.QueueRejects["capacity"] != 1 || m.QueueRejects["missing_glyph"] != 1 {
		t.Fatalf("reject metrics = %#v", m)
	}
	if m.FrameAverage != 2*time.Millisecond || m.FrameP95 != 3*time.Millisecond || m.FrameMax != 3*time.Millisecond {
		t.Fatalf("frame timing metrics = avg %s p95 %s max %s", m.FrameAverage, m.FrameP95, m.FrameMax)
	}
}

func TestTextureDensityPerfCacheEvictionAccounting(t *testing.T) {
	p := newTextureDensityPerf()
	b := RenderSprites{textureEpoch: 1, textureBudget: 1, hdPerf: p}
	img := &Image{c: &b, textureScale: 2}
	img.retainNativeTexture(nativeImage(color.NRGBA{R: 1, A: 255}), nil)
	b.endTextureFrame()
	if p.cacheHighWater == 0 {
		t.Fatal("cache high-water was not recorded")
	}
	b.endTextureFrame()
	if p.cacheEvictions != 1 || p.cacheBytes != 0 {
		t.Fatalf("cache metrics = evictions %d bytes %d", p.cacheEvictions, p.cacheBytes)
	}
}

func TestTextureDensityPerfSnapshotQueueIsCurrentFrame(t *testing.T) {
	r := NewRender(nil, nil)
	r.textureDensity = []textureDensityDraw{{dst: image.Point{}, logical: image.Pt(1, 1)}}
	r.textureDensityBytes = 8
	m := r.TextureDensityMetrics()
	if m.QueueCount != 1 || m.QueueBytes != 8 {
		t.Fatalf("queue metrics = %d/%d", m.QueueCount, m.QueueBytes)
	}
}

func TestTextureDensityTraceIsPerFrameAndReasonCoded(t *testing.T) {
	r := NewRender(nil, nil)
	r.ArmTextureDensityTrace()
	p := r.hdPerf
	p.reject(textureDensityRejectCapacity)
	p.traceAccepted++
	p.traceOwnership += 4
	p.traceCurrent = p.beginTraceRecord(false, image.Rect(2, 3, 4, 5))
	p.traceResult(true)
	p.traceResult(false)

	if !p.trace || p.traceAccepted != 1 || p.traceRejected != 1 || p.traceOwnership != 4 {
		t.Fatalf("trace totals = %#v", p)
	}
	if got := p.traceRecords; len(got) != 1 || got[0].Text || got[0].Bounds != image.Rect(2, 3, 4, 5) || got[0].Visible != 1 || got[0].Suppressed != 1 {
		t.Fatalf("trace records = %#v", got)
	}
}
