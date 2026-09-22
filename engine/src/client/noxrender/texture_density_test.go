package noxrender

import (
	"encoding/binary"
	"image"
	"image/color"
	"runtime"
	"testing"

	noxcolor "github.com/opennox/libs/color"
	"github.com/opennox/libs/noxfont"
	"github.com/opennox/libs/noximage"
)

func textureRaw(width, height int, off image.Point) []byte {
	return textureRawColor(width, height, off, 0xffff)
}

func textureRawColor(width, height int, off image.Point, px uint16) []byte {
	data := make([]byte, 17+height*(2+width*2))
	binary.LittleEndian.PutUint32(data[0:], uint32(width))
	binary.LittleEndian.PutUint32(data[4:], uint32(height))
	binary.LittleEndian.PutUint32(data[8:], uint32(off.X))
	binary.LittleEndian.PutUint32(data[12:], uint32(off.Y))
	cur := 17
	for y := 0; y < height; y++ {
		data[cur] = 5
		data[cur+1] = byte(width)
		cur += 2
		for x := 0; x < width; x++ {
			binary.LittleEndian.PutUint16(data[cur:], px)
			cur += 2
		}
	}
	return data
}

// textureMeta is enough for queue-only tests: textureDensity uses the image
// header for logical geometry before it enters the pixel decoder.
func textureMeta(width, height int, off image.Point) []byte {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:], uint32(width))
	binary.LittleEndian.PutUint32(data[4:], uint32(height))
	binary.LittleEndian.PutUint32(data[8:], uint32(off.X))
	binary.LittleEndian.PutUint32(data[12:], uint32(off.Y))
	return data
}

func textureRawOp(width, height int, off image.Point, op byte, px uint16) []byte {
	data := textureRawColor(width, height, off, px)
	for at := 17; at < len(data); at += 2 + width*2 {
		data[at] = op
	}
	return data
}

func nativeImage(cols ...color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, len(cols), 2))
	for x, c := range cols {
		img.SetNRGBA(x, 0, c)
		img.SetNRGBA(x, 1, c)
	}
	return img
}

func bitmapGlyph(width, height int, on ...image.Point) *noxfont.Bitmap {
	stride := (width + 7) / 8
	g := &noxfont.Bitmap{Pix: make([]byte, stride*height), Stride: stride, Rect: image.Rect(0, 0, width, height)}
	for _, p := range on {
		g.SetAlpha(p.X, p.Y, color.Alpha{A: 255})
	}
	return g
}

func bitmapFace(g *noxfont.Bitmap) *noxfont.Font {
	return &noxfont.Font{Ranges: []noxfont.Range{{StartChar: 'A', EndChar: 'A', Glyphs: []*noxfont.Bitmap{g}}}}
}

func TestParseTextureScale(t *testing.T) {
	if got := parseTextureScale([]byte(`{"texture_scale":2}`)); got != 2 {
		t.Fatalf("scale = %d, want 2", got)
	}
	if got := parseTextureScale([]byte(`{"texture_scale":4}`)); got != 4 {
		t.Fatalf("scale = %d, want 4", got)
	}
	for _, data := range []string{`{}`, `{"texture_scale":1}`, `{"texture_scale":3}`, `{"texture_scale":"2"}`, `{bad`} {
		if got := parseTextureScale([]byte(data)); got != 1 {
			t.Errorf("parseTextureScale(%q) = %d, want 1", data, got)
		}
	}
}

func TestTextureDensityTextPreflightRejectsMissingGlyph(t *testing.T) {
	face := bitmapFace(bitmapGlyph(2, 3, image.Pt(0, 0)))
	if !textureDensityTextSupported(face, "A") {
		t.Fatal("present glyph was rejected")
	}
	if textureDensityTextSupported(face, "AB") {
		t.Fatal("missing glyph was accepted through replacement fallback")
	}
}

func TestTextureCacheBudgetParsing(t *testing.T) {
	if got := parseTextureCacheBudget(""); got != defaultTextureCacheBudget {
		t.Fatalf("default budget = %d", got)
	}
	if got := parseTextureCacheBudget("64"); got != 64<<20 {
		t.Fatalf("64 MiB budget = %d", got)
	}
	for _, value := range []string{"bad", "0", "31", "1025"} {
		if got := parseTextureCacheBudget(value); got != defaultTextureCacheBudget {
			t.Fatalf("invalid %q budget = %d", value, got)
		}
	}
}

func TestTextureCacheEvictsOnlyOldNativeSources(t *testing.T) {
	b := RenderSprites{textureEpoch: 1, textureBudget: 8}
	old := &Image{c: &b, textureScale: 2}
	current := &Image{c: &b, textureScale: 2}

	old.retainNativeTexture(nativeImage(color.NRGBA{R: 1, A: 255}, color.NRGBA{R: 2, A: 255}), nil)
	b.endTextureFrame()
	current.retainNativeTexture(nativeImage(color.NRGBA{G: 1, A: 255}, color.NRGBA{G: 2, A: 255}), nil)
	b.textureBudget = 4
	b.endTextureFrame()

	if old.hasNativeTexture() || old.textureBytes != 0 {
		t.Fatal("old decoded texture was not evicted")
	}
	if !current.hasNativeTexture() || current.textureBytes == 0 {
		t.Fatal("current-frame decoded texture was evicted")
	}
	if current.textureScale != 2 {
		t.Fatal("eviction lost the reload marker")
	}
	if len(b.textureLRU) != 1 || b.textureLRU[0].img != current {
		t.Fatalf("LRU tracking = %v, want only current image", b.textureLRU)
	}
}

func TestTextureCacheEvictsLeastRecentlyUsedFromHeap(t *testing.T) {
	b := RenderSprites{textureEpoch: 1, textureBudget: 1 << 20}
	old := &Image{c: &b, textureScale: 2}
	recent := &Image{c: &b, textureScale: 2}
	old.retainNativeTexture(nativeImage(color.NRGBA{R: 1, A: 255}, color.NRGBA{R: 2, A: 255}), nil)
	recent.retainNativeTexture(nativeImage(color.NRGBA{G: 1, A: 255}, color.NRGBA{G: 2, A: 255}), nil)
	b.endTextureFrame()

	// Touch recent in the newer frame, making old the heap root.
	if !recent.ensureNativeTexture() {
		t.Fatal("recent texture was not available")
	}
	b.textureBudget = recent.textureBytes
	b.endTextureFrame()
	if old.hasNativeTexture() || !recent.hasNativeTexture() {
		t.Fatal("LRU eviction did not retain only the recently used source")
	}
}

func TestTextureCacheTrackingCleanupOnRelease(t *testing.T) {
	b := RenderSprites{textureEpoch: 1}
	img := &Image{c: &b, textureScale: 2}
	img.retainNativeTexture(nativeImage(color.NRGBA{R: 1, A: 255}, color.NRGBA{R: 2, A: 255}), nil)
	if len(b.textureLRU) != 1 {
		t.Fatalf("tracked entries = %d, want 1", len(b.textureLRU))
	}
	img.releaseNativeTexture()
	if len(b.textureLRU) != 0 || b.textureBytes != 0 || img.textureLRU != nil {
		t.Fatalf("tracking cleanup failed: heap=%d bytes=%d entry=%v", len(b.textureLRU), b.textureBytes, img.textureLRU)
	}
}

func TestTextureDensityPreservesNativeSamplesAndAnchor(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 5, 2)))
	img := &Image{
		typ:          5,
		raw:          textureRaw(2, 1, image.Pt(1, 0)),
		texture:      nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}),
		textureScale: 2,
	}
	r.DrawImage16(img, image.Pt(1, 0))
	out := r.CopyPresentationBuffer()
	if out.Rect.Dx() != 10 || out.Rect.Dy() != 4 {
		t.Fatalf("presentation size = %v, want 10x4", out.Rect)
	}
	// The logical offset is one pixel, so the native sprite starts at x=4.
	got := []uint16{out.Pix[4], out.Pix[5], out.Pix[6], out.Pix[7]}
	if got[0] == got[1] || got[1] == got[2] || got[2] == got[3] {
		t.Fatalf("native samples were collapsed: %v", got)
	}
	if r.HasTextureDensityDraws() {
		t.Fatal("texture queue was not reset after presentation")
	}
	r.DrawImage16(img, image.Pt(1, 0))
	if gotAgain := r.CopyPresentationBuffer(); gotAgain != out {
		t.Fatal("2x presentation buffer was not reused")
	}
}

func TestTextureDensityPackedOpaqueSourceReplaysNativeSamples(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	img := &Image{typ: 5, raw: textureRaw(2, 1, image.Point{}), textureScale: 2}
	img.retainNativeTexture(nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}), nil)
	if img.texture != nil || len(img.texture16) != 8 {
		t.Fatalf("opaque source was not packed: texture=%T pixels=%d", img.texture, len(img.texture16))
	}
	if r.textureDensityNeedsBackground(img) {
		t.Fatal("opaque packed draw unexpectedly retained a pre-draw background")
	}
	r.DrawImage16(img, image.Point{})
	out := r.CopyPresentationBuffer()
	if out.Pix[0] == out.Pix[1] || out.Pix[1] == out.Pix[2] || out.Pix[2] == out.Pix[3] {
		t.Fatalf("packed native samples were collapsed: %v", out.Pix[:4])
	}
}

func TestTextureDensityMenuHoverFitsAfterPackedBackdrop(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.texturePresentationScale = 2
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 640, 480)))

	packed := func(size, off image.Point) *Image {
		bounds := image.Rect(0, 0, size.X*2, size.Y*2)
		return &Image{
			raw:           textureMeta(size.X, size.Y, off),
			texture16:     make([]uint16, bounds.Dx()*bounds.Dy()),
			textureBounds: bounds,
			textureScale:  2,
		}
	}
	alpha := func(size, off image.Point) *Image {
		return &Image{
			raw:          textureMeta(size.X, size.Y, off),
			texture:      image.NewNRGBA(image.Rect(0, 0, size.X*2, size.Y*2)),
			textureScale: 2,
		}
	}
	queueAlpha := func(img *Image, pos image.Point) {
		bg := r.captureTextureBackground(img, pos)
		if len(bg) == 0 {
			t.Fatalf("missing menu snapshot at %v", pos)
		}
		r.queueTextureDensity(img, pos, bg)
	}

	// MainBG is opaque; its old, unused pre-draw copy was 600 KiB of the
	// 4 MiB frame budget. The remaining entries mirror the five main-menu
	// button panels plus the bottom "Play Intro" hover overlay.
	backdrop := packed(image.Pt(640, 480), image.Point{})
	if r.textureDensityNeedsBackground(backdrop) {
		t.Fatal("opaque main-menu backdrop unexpectedly needs a snapshot")
	}
	r.queueTextureDensity(backdrop, image.Point{}, nil)
	queueAlpha(alpha(image.Pt(248, 162), image.Pt(136, 0)), image.Pt(-20, -30))
	queueAlpha(alpha(image.Pt(278, 222), image.Pt(161, 0)), image.Pt(-20, -30))
	queueAlpha(alpha(image.Pt(278, 252), image.Pt(184, 0)), image.Point{})
	queueAlpha(alpha(image.Pt(274, 249), image.Pt(203, 231)), image.Pt(20, 30))
	queueAlpha(alpha(image.Pt(248, 189), image.Pt(256, 291)), image.Pt(20, 30))
	queueAlpha(alpha(image.Pt(200, 53), image.Pt(240, 231)), image.Pt(20, 30))

	if got, want := len(r.textureDensity), 7; got != want {
		t.Fatalf("menu hover queued %d native records, want %d (used %d / %d bytes)", got, want, r.textureDensityBytes, textureDensityMaxBytes)
	}
	if r.textureDensityBytes >= textureDensityMaxBytes {
		t.Fatalf("menu hover exhausted the native queue: %d / %d bytes", r.textureDensityBytes, textureDensityMaxBytes)
	}
}

func TestTextureDensityMenuHoverReusesFrameBuffers(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	img := &Image{typ: 5, raw: textureRaw(2, 1, image.Point{}), texture: nativeImage(
		color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255},
		color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255},
	), textureScale: 2}
	r.DrawImage16(img, image.Point{})
	if len(r.textureDensity) != 1 {
		t.Fatalf("first frame records = %d, want 1", len(r.textureDensity))
	}
	firstSnapshot := &r.textureDensity[0].snapshot[0]
	firstOwners := &r.textureDensity[0].owners[0]
	r.CopyPresentationBuffer()
	r.DrawImage16(img, image.Point{})
	if len(r.textureDensity) != 1 {
		t.Fatalf("second frame records = %d, want 1", len(r.textureDensity))
	}
	if &r.textureDensity[0].snapshot[0] != firstSnapshot || &r.textureDensity[0].owners[0] != firstOwners {
		t.Fatal("menu record allocated new snapshot storage on the next frame")
	}
}

func TestTextureDensityNRGBA2xFastPathMatchesGeneric(t *testing.T) {
	makeRenderer := func() *NoxRender {
		r := NewRender(nil, nil)
		d, free := NewRenderData()
		t.Cleanup(free)
		r.SetData(d)
		r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
		r.PixBuffer().Pix[0] = uint16(noxcolor.RGB5551Color(255, 0, 0))
		r.PixBuffer().Pix[1] = uint16(noxcolor.RGB5551Color(0, 255, 0))
		return r
	}
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	colors := []color.NRGBA{
		{R: 255, A: 0}, {R: 255, G: 64, A: 64}, {R: 255, G: 128, A: 128}, {R: 255, G: 255, A: 255},
		{B: 255, A: 255}, {B: 255, G: 128, A: 128}, {B: 255, G: 64, A: 64}, {B: 255, A: 0},
	}
	for i, c := range colors {
		src.Pix[i*4+0] = c.R
		src.Pix[i*4+1] = c.G
		src.Pix[i*4+2] = c.B
		src.Pix[i*4+3] = c.A
	}
	fast := makeRenderer()
	slow := makeRenderer()
	record := func(r *NoxRender) textureDensityDraw {
		pix := append([]uint16(nil), r.PixBuffer().Pix...)
		return textureDensityDraw{
			src:        src,
			srcBounds:  src.Rect,
			scale:      2,
			logical:    image.Pt(2, 1),
			snapshot:   pix,
			owners:     make([]uint32, len(pix)),
			background: append([]uint16(nil), pix...),
		}
	}
	fastOut := fast.scaledTexturePresentation(fast.PixBuffer(), 2)
	fast.clearTextureReplayWritten(fastOut)
	fast.replayTextureDensityNRGBA(fastOut, record(fast))
	slowOut := slow.scaledTexturePresentation(slow.PixBuffer(), 2)
	slow.clearTextureReplayWritten(slowOut)
	slow.replayTextureDensityNRGBAGeneric(slowOut, record(slow), image.Rectangle{}, src)
	if len(fastOut.Pix) != len(slowOut.Pix) {
		t.Fatalf("fast pixels = %d, generic = %d", len(fastOut.Pix), len(slowOut.Pix))
	}
	for i := range fastOut.Pix {
		if fastOut.Pix[i] != slowOut.Pix[i] {
			t.Fatalf("fast pixel %d = %x, generic = %x", i, fastOut.Pix[i], slowOut.Pix[i])
		}
	}
}

func TestTextureDensityDirtyPresentationMatchesFullReplay(t *testing.T) {
	baseColor := uint16(noxcolor.RGB5551Color(0, 0, 255))
	logicalSprite := uint16(noxcolor.RGB5551Color(255, 0, 0))
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i+0] = 255
		src.Pix[i+3] = 128
	}
	makeRenderer := func() *NoxRender {
		r := NewRender(nil, nil)
		d, free := NewRenderData()
		t.Cleanup(free)
		r.SetData(d)
		r.texturePresentationScale = 2
		r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 48, 16)))
		for i := range r.PixBuffer().Pix {
			r.PixBuffer().Pix[i] = baseColor
		}
		r.textureOwners = make([]uint32, len(r.PixBuffer().Pix))
		return r
	}
	queueSprite := func(r *NoxRender) {
		r.PixBuffer().Pix[0] = logicalSprite
		r.textureDensity = append(r.textureDensity, textureDensityDraw{
			src:        src,
			srcBounds:  src.Rect,
			scale:      2,
			logical:    image.Pt(1, 1),
			snapshot:   []uint16{logicalSprite},
			owners:     []uint32{0},
			background: []uint16{baseColor},
		})
	}
	dirty := makeRenderer()
	full := makeRenderer()
	queueSprite(dirty)
	queueSprite(full)
	dirty.CopyPresentationBuffer()
	full.CopyPresentationBuffer()

	// A later logical primitive claims the sprite's pixel without changing its
	// RGB555 value. Ownership alone must dirty the tile and suppress the old HD
	// replay, exactly as a full presentation rebuild does.
	dirty.textureOwners[0] = 1
	full.textureOwners[0] = 1
	queueSprite(dirty)
	queueSprite(full)
	if regions, redraw := dirty.textureDensityDirtyRegions(dirty.PixBuffer(), 2); redraw || len(regions) != 1 {
		t.Fatalf("dirty plan = %d regions, full=%v; want one partial tile", len(regions), redraw)
	}
	dirtyOut := dirty.CopyPresentationBuffer()
	full.texturePresentationValid = false
	fullOut := full.CopyPresentationBuffer()
	for i := range dirtyOut.Pix {
		if dirtyOut.Pix[i] != fullOut.Pix[i] {
			t.Fatalf("dirty output pixel %d = %x, full replay = %x", i, dirtyOut.Pix[i], fullOut.Pix[i])
		}
	}
}

func TestTextureDensityParallelDirtyTilesMatchSequentialReplay(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("parallel replay requires at least two CPUs")
	}
	makeRenderer := func() *NoxRender {
		r := NewRender(nil, nil)
		d, free := NewRenderData()
		t.Cleanup(free)
		r.SetData(d)
		r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 64, 64)))
		for i := range r.PixBuffer().Pix {
			r.PixBuffer().Pix[i] = uint16(noxcolor.RGB5551Color(0, 0, 255))
		}
		return r
	}
	source := func(c color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i+0] = c.R
			img.Pix[i+1] = c.G
			img.Pix[i+2] = c.B
			img.Pix[i+3] = c.A
		}
		return img
	}
	appendRecords := func(r *NoxRender) {
		base := append([]uint16(nil), r.PixBuffer().Pix...)
		for _, src := range []*image.NRGBA{source(color.NRGBA{R: 255, A: 128}), source(color.NRGBA{G: 255, A: 128})} {
			r.textureDensity = append(r.textureDensity, textureDensityDraw{
				src:        src,
				srcBounds:  src.Rect,
				scale:      2,
				logical:    image.Pt(64, 64),
				snapshot:   append([]uint16(nil), base...),
				owners:     make([]uint32, len(base)),
				background: append([]uint16(nil), base...),
			})
		}
	}
	regions := make([]image.Rectangle, 0, 16)
	for y := 0; y < 64; y += 16 {
		for x := 0; x < 64; x += 16 {
			regions = append(regions, image.Rect(x, y, x+16, y+16))
		}
	}
	parallel := makeRenderer()
	sequential := makeRenderer()
	appendRecords(parallel)
	appendRecords(sequential)
	if !parallel.shouldParallelTextureDensityReplay(regions) {
		t.Fatal("large dirty workload unexpectedly stayed sequential")
	}
	parallelOut := parallel.scaledTexturePresentation(parallel.PixBuffer(), 2)
	parallel.clearTextureReplayWritten(parallelOut)
	parallel.replayTextureDensityRegions(parallelOut, regions)
	sequentialOut := sequential.scaledTexturePresentation(sequential.PixBuffer(), 2)
	sequential.clearTextureReplayWritten(sequentialOut)
	for _, d := range sequential.textureDensity {
		for _, rc := range regions {
			sequential.replayTextureDensityRegion(sequentialOut, d, rc)
		}
	}
	for i := range parallelOut.Pix {
		if parallelOut.Pix[i] != sequentialOut.Pix[i] {
			t.Fatalf("parallel pixel %d = %x, sequential = %x", i, parallelOut.Pix[i], sequentialOut.Pix[i])
		}
		if parallel.textureReplayWritten[i] != sequential.textureReplayWritten[i] {
			t.Fatalf("parallel replay marker %d differs", i)
		}
	}
}

func TestTextureDensityPackedOpaqueSourcePreservesAlphaDraw(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	d.SetAlphaEnabled(true)
	d.SetAlpha(0x80)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	background := uint16(noxcolor.RGB5551Color(0, 0, 255))
	r.PixBuffer().Pix[0] = background
	img := &Image{typ: 5, raw: textureRawColor(1, 1, image.Point{}, 0xf008), textureScale: 2}
	img.retainNativeTexture(nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{R: 255, A: 255}), nil)
	if !r.textureDensityNeedsBackground(img) {
		t.Fatal("alpha-blended packed draw lost its required pre-draw background")
	}
	r.DrawImage16(img, image.Point{})
	out := r.CopyPresentationBuffer()
	want := blendTexturePixel(background, Color16{R: 255}, 128)
	for i, got := range out.Pix {
		if got != want {
			t.Fatalf("packed alpha replay at pixel %d = %x, want %x", i, got, want)
		}
	}
}

func TestTextureDensityNRGBARespectsRenderAlpha(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	d.SetAlphaEnabled(true)
	d.SetAlpha(0x80)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	background := uint16(noxcolor.RGB5551Color(0, 0, 255))
	r.PixBuffer().Pix[0] = background
	img := &Image{typ: 5, raw: textureRawColor(1, 1, image.Point{}, 0xf008), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{R: 255, A: 255}), textureScale: 2}
	r.DrawImage16(img, image.Point{})
	out := r.CopyPresentationBuffer()
	want := blendTexturePixel(background, Color16{R: 255}, 128)
	for i, got := range out.Pix {
		if got != want {
			t.Fatalf("NRGBA alpha replay at pixel %d = %x, want %x", i, got, want)
		}
	}
}

func TestTextureDensityReplaysNativeFontSprite(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.texturePresentationScale = 2
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	d.SetTextColor(color.NRGBA{R: 255, G: 0, B: 0, A: 255})

	logical := bitmapFace(bitmapGlyph(1, 1, image.Pt(0, 0)))
	// The HD glyph deliberately occupies only one physical subpixel. If the
	// base 1x text were merely enlarged, all four pixels would be red.
	native := bitmapFace(bitmapGlyph(2, 2, image.Pt(0, 0)))
	background := r.captureTextureBackgroundRect(image.Rect(0, 0, 1, 1))
	r.PixBuffer().Pix[0] = Color16{R: 255}.Make16()
	if !r.queueTextureText(logical, native, "A", image.Point{}, image.Rect(0, 0, 1, 1), background, 0) {
		t.Fatal("native font did not enter the 2x queue")
	}
	out := r.CopyPresentationBuffer()
	if got, want := out.Pix[0], (Color16{R: 255}).Make16(); got != want {
		t.Fatalf("native font pixel = %x, want %x", got, want)
	}
	for _, ind := range []int{1, 2, 3} {
		if out.Pix[ind] != 0 {
			t.Fatalf("logical text was still block-scaled at pixel %d: %x", ind, out.Pix[ind])
		}
	}
}

func TestTextureDensityNativeFontRespectsLaterLogicalDraw(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.texturePresentationScale = 2
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	d.SetTextColor(color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	logical := bitmapFace(bitmapGlyph(1, 1, image.Pt(0, 0)))
	native := bitmapFace(bitmapGlyph(2, 2, image.Pt(0, 0)))
	background := r.captureTextureBackgroundRect(image.Rect(0, 0, 1, 1))
	r.PixBuffer().Pix[0] = Color16{R: 255}.Make16()
	if !r.queueTextureText(logical, native, "A", image.Point{}, image.Rect(0, 0, 1, 1), background, 0) {
		t.Fatal("native font did not enter the 2x queue")
	}
	// A later logical draw owns this position, so it must suppress the queued
	// native red glyph even if text replay happens at the end of the frame.
	r.PixBuffer().Pix[0] = Color16{B: 255}.Make16()
	out := r.CopyPresentationBuffer()
	for i, got := range out.Pix {
		if want := (Color16{B: 255}).Make16(); got != want {
			t.Fatalf("later draw not preserved at pixel %d: got %x want %x", i, got, want)
		}
	}
}

func TestTextureDensityScale4PreservesNativeSamples(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	native := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	native.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	native.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	native.SetNRGBA(2, 0, color.NRGBA{B: 255, A: 255})
	native.SetNRGBA(3, 0, color.NRGBA{R: 255, G: 255, A: 255})
	marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: native, textureScale: 4}
	r.DrawImage16(marked, image.Point{})
	out := r.CopyPresentationBuffer()
	if out.Rect.Dx() != 4 || out.Rect.Dy() != 4 {
		t.Fatalf("presentation size = %v, want 4x4", out.Rect)
	}
	if out.Pix[0] == out.Pix[1] || out.Pix[1] == out.Pix[2] || out.Pix[2] == out.Pix[3] {
		t.Fatalf("4x native samples were collapsed: %v", out.Pix[:4])
	}
}

func TestTextureDensityModeKeepsDisplayPresentationStable(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.textureDensityConfiguredScale = 4
	base := noximage.NewImage16(image.Rect(0, 0, 2, 1))
	r.SetPixBuffer(base)
	native := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	native.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	native.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	native.SetNRGBA(2, 0, color.NRGBA{B: 255, A: 255})
	native.SetNRGBA(3, 0, color.NRGBA{R: 255, G: 255, A: 255})
	for x := 0; x < 4; x++ {
		native.SetNRGBA(x, 1, native.NRGBAAt(x, 0))
		native.SetNRGBA(x, 2, native.NRGBAAt(x, 0))
		native.SetNRGBA(x, 3, native.NRGBAAt(x, 0))
	}
	marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: native, textureScale: 4}
	display := image.Pt(8, 3)

	r.SetTextureDensityMode(true)
	r.DrawImage16(marked, image.Point{})
	if !r.HasTextureDensityDraws() {
		t.Fatal("menu mode did not queue the configured native texture draw")
	}
	if got := r.CopyPresentationBuffer(display); got.Rect.Dx() != 8 || got.Rect.Dy() != 3 {
		t.Fatalf("menu presentation = %v, want display 8x3 buffer", got.Rect)
	}

	r.SetTextureDensityMode(false)
	r.DrawImage16(marked, image.Point{})
	if got := r.CopyPresentationBuffer(display); got.Rect.Dx() != 8 || got.Rect.Dy() != 3 {
		t.Fatalf("game presentation = %v, want stable display 8x3 buffer", got.Rect)
	}
	// A frame with no marked sprite must keep the same output size.
	if got := r.CopyPresentationBuffer(display); got.Rect.Dx() != 8 || got.Rect.Dy() != 3 {
		t.Fatalf("unmarked gameplay presentation = %v, want stable display 8x3 buffer", got.Rect)
	}
}

func TestTextureDensityScale1KeepsLogicalPresentation(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	base := noximage.NewImage16(image.Rect(0, 0, 1, 1))
	r.SetPixBuffer(base)
	r.DrawImage16(&Image{typ: 5, raw: textureRaw(1, 1, image.Point{})}, image.Point{})
	if len(r.textureOwners) != 0 {
		t.Fatal("scale-1 draw allocated ownership map")
	}
	if got := r.CopyPresentationBuffer(); got != base || got.Rect.Dx() != 1 || got.Rect.Dy() != 1 {
		t.Fatal("unmarked scale-1 draw changed presentation path")
	}
}

func TestTextureDensityAlphaUsesPreDrawBackground(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	bg := uint16(noxcolor.RGB5551Color(0, 0, 255))
	r.PixBuffer().Pix[0] = bg
	marked := &Image{typ: 5, raw: textureRawColor(1, 1, image.Point{}, 0xf008), texture: nativeImage(color.NRGBA{R: 255, A: 128}, color.NRGBA{R: 255, A: 128}), textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	out := r.CopyPresentationBuffer()
	want := blendTexturePixel(bg, Color16{R: 255}, 128)
	if out.Pix[0] != want || out.Pix[1] != want {
		t.Fatalf("alpha replay = %x/%x, want one blend %x", out.Pix[0], out.Pix[1], want)
	}
}

func TestTextureDensityTransparentNativeSubpixelUsesBackground(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	bg := uint16(noxcolor.RGB5551Color(0, 0, 255))
	r.PixBuffer().Pix[0] = bg
	native := nativeImage(color.NRGBA{A: 0}, color.NRGBA{G: 255, A: 255})
	marked := &Image{typ: 5, raw: textureRawColor(1, 1, image.Point{}, 0xf00f), texture: native, textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	out := r.CopyPresentationBuffer()
	if out.Pix[0] != bg || out.Pix[1] == bg {
		t.Fatalf("transparent native subpixel = %x/%x, want background/opaque sample", out.Pix[0], out.Pix[1])
	}
}

func TestTextureDensityTransparentAndNoopRunsDoNotOwn(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{name: "transparent", raw: textureRawColor(1, 1, image.Point{}, 0xf000)},
		{name: "noop", raw: textureRawOp(1, 1, image.Point{}, 6, 0xffff)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRender(nil, nil)
			d, free := NewRenderData()
			defer free()
			r.SetData(d)
			r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
			marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{R: 0, G: 255, A: 255}), textureScale: 2}
			r.DrawImage16(marked, image.Point{})
			later := &Image{typ: 5, raw: tc.raw}
			r.DrawImage16(later, image.Point{})
			out := r.CopyPresentationBuffer()
			if out.Pix[0] == out.Pix[1] {
				t.Fatal("transparent/noop run incorrectly masked native replay")
			}
		})
	}
}

func TestTextureDensityCutAndInterlaceFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*NoxRender)
	}{
		{name: "cut", set: func(r *NoxRender) { r.Set_dword_5d4594_3799484(1) }},
		{name: "interlace", set: func(r *NoxRender) { r.SetInterlacing(true, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRender(nil, nil)
			d, free := NewRenderData()
			defer free()
			r.SetData(d)
			r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
			tc.set(r)
			marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}), textureScale: 2}
			r.DrawImage16(marked, image.Point{})
			if r.HasTextureDensityDraws() {
				t.Fatal("cut/interlaced draw entered HD queue")
			}
		})
	}
}

func TestTextureDensityPrimitiveMasksOnlyOverlap(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	marked := &Image{typ: 5, raw: textureRaw(2, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}), textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	r.DrawRectFilledOpaque(0, 0, 1, 1, color.Black)
	out := r.CopyPresentationBuffer()
	if out.Pix[0] != out.Pix[1] {
		t.Fatal("primitive overlap did not keep its logical pixels")
	}
	if out.Pix[2] == out.Pix[3] {
		t.Fatal("primitive overlap disabled native replay outside its rectangle")
	}
}

func TestTextureDensityNonOverlappingPrimitiveKeepsReplay(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 3, 1)))
	marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}), textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	r.DrawRectFilledOpaque(2, 0, 1, 1, color.Black)
	out := r.CopyPresentationBuffer()
	if out.Pix[0] == out.Pix[1] {
		t.Fatal("non-overlapping primitive disabled native replay")
	}
}

func TestTextureDensityMaterialBoundsMustMatch(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	mat := image.NewPaletted(image.Rect(0, 0, 2, 4), color.Palette{color.Transparent})
	if textureSourceValid(src, mat) {
		t.Fatal("mismatched _mat bounds accepted for scale-2 source")
	}
}

func TestTextureDensityRejectsOddNativeDimensions(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	odd := image.NewNRGBA(image.Rect(0, 0, 5, 2))
	marked := &Image{typ: 5, raw: textureRaw(2, 1, image.Point{}), texture: odd, textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	if r.HasTextureDensityDraws() {
		t.Fatal("odd native dimensions unexpectedly entered the 2x queue")
	}
}

func TestTextureDensityClipsNativeReplay(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	d.SetClip(true)
	d.SetClipRect(image.Rect(1, 0, 2, 1))
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	marked := &Image{typ: 5, raw: textureRaw(2, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}), textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	out := r.CopyPresentationBuffer()
	// Only the second logical pixel is eligible for native replay.
	if out.Pix[0] == out.Pix[2] || out.Pix[2] == out.Pix[3] {
		t.Fatal("clip edge did not preserve native samples")
	}
}

func TestTextureDensityMasksLaterLogicalDraw(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 2, 1)))
	marked := &Image{typ: 5, raw: textureRaw(1, 1, image.Point{}), texture: nativeImage(color.NRGBA{R: 255, A: 255}, color.NRGBA{R: 255, A: 255}), textureScale: 2}
	r.DrawImage16(marked, image.Point{})
	// This unmarked draw owns the logical pixel after the HD draw.
	logical := &Image{typ: 5, raw: textureRawColor(1, 1, image.Point{}, 0xf00f)}
	r.DrawImage16(logical, image.Point{})
	out := r.CopyPresentationBuffer()
	if out.Pix[0] != out.Pix[1] {
		t.Fatal("masked logical pixel was not kept across 2x replay")
	}
}

func TestTextureDensityMaterialAndAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 128, B: 128, A: 128})
	mat := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Transparent, color.White, color.White})
	// Decoded PCX masks shift the engine material slot by +1.
	mat.SetColorIndex(0, 0, 2)
	d := RenderData{}
	d.Reset()
	d.SetMaterialRGB(1, 255, 0, 0)
	rec := textureDensityDraw{src: src, mat: mat, state: d}
	c, alpha, ok := rec.sampleColor(0, 0)
	if !ok || alpha != 128 || c.R != 127 || c.G != 0 || c.B != 0 {
		t.Fatalf("sample = %#v alpha=%d ok=%v, want half-intensity material red with source alpha", c, alpha, ok)
	}
}

func TestTextureDensityNRGBAReplayPreservesMaterialAndAlpha(t *testing.T) {
	r := NewRender(nil, nil)
	d, free := NewRenderData()
	defer free()
	r.SetData(d)
	r.texturePresentationScale = 2
	r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 1, 1)))
	background := uint16(noxcolor.RGB5551Color(0, 0, 255))
	r.PixBuffer().Pix[0] = background
	d.SetAlphaEnabled(true)
	d.SetAlpha(0x80)
	d.SetMaterialRGB(1, 255, 0, 0)
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	mat := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Transparent, color.White, color.White})
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 128, G: 128, B: 128, A: 128})
			mat.SetColorIndex(x, y, 2)
		}
	}
	rec := textureDensityDraw{
		src:        src,
		mat:        mat,
		srcBounds:  src.Bounds(),
		scale:      2,
		logical:    image.Pt(1, 1),
		snapshot:   []uint16{background},
		owners:     []uint32{0},
		background: []uint16{background},
		state:      *d,
	}
	c, alpha, ok := rec.sampleColor(0, 0)
	if !ok {
		t.Fatal("generic material sample was unavailable")
	}
	r.textureDensity = []textureDensityDraw{rec}
	out := r.CopyPresentationBuffer()
	want := blendTexturePixel(background, c, alpha)
	for i, got := range out.Pix {
		if got != want {
			t.Fatalf("NRGBA material replay at pixel %d = %x, want %x", i, got, want)
		}
	}
}

func TestTextureDensitySkinMaterialUsesEngineSlotSix(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
	mat := image.NewPaletted(image.Rect(0, 0, 1, 1), make(color.Palette, 8))
	mat.SetColorIndex(0, 0, 7) // decoded mask index for engine material 6 (skin)
	d := RenderData{}
	d.Reset()
	d.SetMaterialRGB(6, 240, 160, 80)
	d.SetMaterialRGB(7, 0, 0, 0)
	c, alpha, ok := (textureDensityDraw{src: src, mat: mat, state: d}).sampleColor(0, 0)
	if !ok || alpha != 255 || c.R != 187 || c.G != 125 || c.B != 62 {
		t.Fatalf("skin sample = %#v alpha=%d ok=%v, want intensity-scaled material slot 6", c, alpha, ok)
	}
}
