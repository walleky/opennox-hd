package noxrender

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"reflect"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/opennox/libs/noximage"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func configuredTextureDensityScale() int {
	switch os.Getenv("NOX_TEXTURE_DENSITY_SCALE") {
	case "2":
		return 2
	case "4":
		return 4
	default:
		return 0
	}
}

// SetTextureDensityMode selects a stable native presentation path for the
// current video mode. The launcher-selected native texture density is used in
// both menus and gameplay on every frame, including frames with no eligible
// sprite draw. CopyPresentationBuffer then fits that frame to the actual
// display size, so menu aspect changes cannot make the output surface switch
// or extend past the screen.
func (r *NoxRender) SetTextureDensityMode(_ bool) {
	if r == nil {
		return
	}
	if supportedTextureScale(r.textureDensityConfiguredScale) {
		r.textureDensityEnabled = true
		r.texturePresentationScale = r.textureDensityConfiguredScale
	} else {
		// Direct executable/test callers without launcher configuration retain
		// the original per-frame scale discovery behavior.
		r.textureDensityEnabled = true
		r.texturePresentationScale = 0
	}
	r.texturePresentationValid = false
	r.resetTextureDensity()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (r *NoxRender) nextTextureOwner() uint32 {
	if r == nil || r.pix == nil {
		return 0
	}
	if len(r.textureOwners) != len(r.pix.Pix) {
		r.textureOwners = make([]uint32, len(r.pix.Pix))
	}
	r.textureOwnerSeq++
	if r.textureOwnerSeq == 0 {
		for i := range r.textureOwners {
			r.textureOwners[i] = 0
		}
		r.textureOwnerSeq = 1
	}
	return r.textureOwnerSeq
}

func (r *NoxRender) markTextureOwnership(img Image16, pos image.Point, track bool) {
	if r == nil || r.pix == nil || img == nil || !track {
		return
	}
	data := img.Pixdata()
	if len(data) < 17 {
		return
	}
	w := int(binary.LittleEndian.Uint32(data[0:]))
	h := int(binary.LittleEndian.Uint32(data[4:]))
	off := image.Point{
		X: int(int32(binary.LittleEndian.Uint32(data[8:]))),
		Y: int(int32(binary.LittleEndian.Uint32(data[12:]))),
	}
	if w <= 0 || h <= 0 {
		return
	}
	owner := r.nextTextureOwner()
	if owner == 0 {
		return
	}
	clip := image.Rectangle{Min: pos.Add(off), Max: pos.Add(off).Add(image.Pt(w, h))}.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		clip = clip.Intersect(r.p.ClipRect())
	}
	if clip.Empty() {
		return
	}
	mark := func(x, y int) {
		if x >= clip.Min.X && x < clip.Max.X && y >= clip.Min.Y && y < clip.Max.Y {
			r.textureOwners[y*r.pix.Stride+x] = owner
		}
	}
	data = data[17:]
	if img.Type()&0x3f == 2 || img.Type()&0x3f == 7 {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				mark(pos.X+off.X+x, pos.Y+off.Y+y)
				if len(data) >= 2 {
					data = data[2:]
				}
			}
		}
		return
	}
	for y := 0; y < h; y++ {
		x := 0
		for x < w && len(data) >= 2 {
			op := data[0]
			n := int(data[1])
			data = data[2:]
			if n <= 0 || n > w-x {
				return
			}
			if op&0xf != 1 && op&0xf != 6 {
				for i := 0; i < n; i++ {
					write := true
					if op&0xf == 5 {
						if len(data) < 2*(i+1) {
							return
						}
						write = binary.LittleEndian.Uint16(data[2*i:])&0xF != 0
					}
					if write {
						mark(pos.X+off.X+x+i, pos.Y+off.Y+y)
					}
				}
			}
			if op&0xf == 4 {
				if len(data) < n {
					return
				}
				data = data[n:]
			} else {
				if len(data) < 2*n {
					return
				}
				data = data[2*n:]
			}
			x += n
		}
	}
}

func (r *NoxRender) textureDensityEligible(img *Image) bool {
	if r == nil || !r.textureDensityEnabled || img == nil || !supportedTextureScale(int(img.textureScale)) || r.dword_5d4594_3799484 != 0 || r.interlacing {
		return false
	}
	if r.texturePresentationScale > 1 && int(img.textureScale) != r.texturePresentationScale {
		return false
	}
	if r.textureDensityScale != 0 && r.textureDensityScale != int(img.textureScale) {
		return false
	}
	if !img.ensureNativeTexture() {
		return false
	}
	return img.nativeTextureValidAtScale(int(img.textureScale))
}

// Primitive writes do not carry the compressed sprite ownership stream. Mark
// only their logical rectangle as newer so an unrelated hover/UI redraw cannot
// discard every native replay in the frame.
func (r *NoxRender) markTexturePrimitiveRect(rc image.Rectangle) {
	if r == nil || r.pix == nil || len(r.textureDensity) == 0 || rc.Empty() {
		return
	}
	rc = rc.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		rc = rc.Intersect(r.p.ClipRect())
	}
	if rc.Empty() {
		return
	}
	for _, d := range r.textureDensity {
		if rc.Intersect(r.textureDensityClip(d)).Empty() {
			continue
		}
		owner := r.nextTextureOwner()
		for y := rc.Min.Y; y < rc.Max.Y; y++ {
			for x := rc.Min.X; x < rc.Max.X; x++ {
				r.textureOwners[y*r.pix.Stride+x] = owner
			}
		}
		break
	}
	if r.hdPerf != nil && r.hdPerf.trace {
		r.hdPerf.traceOwnership += uint64(rc.Dx() * rc.Dy())
	}
}

func (r *NoxRender) captureTextureBackground(img *Image, pos image.Point) []uint16 {
	if r == nil || r.pix == nil || img == nil {
		return nil
	}
	off, logical, ok := img.Meta()
	if !ok {
		return nil
	}
	clip := image.Rectangle{Min: pos.Add(off), Max: pos.Add(off).Add(logical)}.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		clip = clip.Intersect(r.p.ClipRect())
	}
	if clip.Empty() {
		return nil
	}
	out := r.textureDensityBackground(len(r.textureDensity), clip.Dx()*clip.Dy())
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		copy(out[(y-clip.Min.Y)*clip.Dx():], r.pix.Pix[y*r.pix.Stride+clip.Min.X:y*r.pix.Stride+clip.Max.X])
	}
	return out
}

func (r *NoxRender) captureTextureBackgroundRect(rc image.Rectangle) []uint16 {
	if r == nil || r.pix == nil {
		return nil
	}
	rc = rc.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		rc = rc.Intersect(r.p.ClipRect())
	}
	if rc.Empty() {
		return nil
	}
	out := r.textureDensityBackground(len(r.textureDensity), rc.Dx()*rc.Dy())
	for y := rc.Min.Y; y < rc.Max.Y; y++ {
		copy(out[(y-rc.Min.Y)*rc.Dx():], r.pix.Pix[y*r.pix.Stride+rc.Min.X:y*r.pix.Stride+rc.Max.X])
	}
	return out
}

// textureDensityDraw is a bounded, per-frame replay record. The logical draw
// has already happened in PixBuffer; the record only supplies the native
// samples for the presentation pass.
type textureDensityDraw struct {
	src        image.Image
	mat        *image.Paletted
	src16      []uint16
	srcBounds  image.Rectangle
	scale      int
	dst        image.Point // logical top-left, after the logical image offset
	logical    image.Point
	snapshot   []uint16 // logical pixels immediately after this draw
	owners     []uint32 // logical ownership immediately after this draw
	background []uint16 // logical pixels immediately before this draw
	state      RenderData
	text       *textureDensityText
}

// textureDensityBuffers owns reusable, per-record frame storage. Main-menu
// hover needs several large records; allocating their snapshots on every
// frame turns pointer movement and sparks into GC stalls.
type textureDensityBuffers struct {
	snapshot   []uint16
	owners     []uint32
	background []uint16
}

// textureDensityReplayKey contains only stable replay inputs. Snapshots and
// ownership numbers deliberately do not participate: they are per-frame
// masking state, while the logical-frame comparison separately invalidates
// any pixel whose authoritative base changed.
type textureDensityReplayKey struct {
	src, packed uintptr
	mat         *image.Paletted
	srcBounds   image.Rectangle
	scale       int
	dst         image.Point
	logical     image.Point
	state       RenderData
	text        bool
	unknownSrc  bool
}

func textureDensityImageIdentity(src image.Image) uintptr {
	if src == nil {
		return 0
	}
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.UnsafePointer {
		return v.Pointer()
	}
	// A non-pointer image implementation cannot be safely identity-compared.
	// Returning zero makes the key conservative when its other inputs differ.
	return 0
}

func textureDensitySliceIdentity(src []uint16) uintptr {
	if len(src) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(src)))
}

func textureDensityReplayKeyOf(d textureDensityDraw) textureDensityReplayKey {
	srcID := textureDensityImageIdentity(d.src)
	return textureDensityReplayKey{
		src:        srcID,
		packed:     textureDensitySliceIdentity(d.src16),
		mat:        d.mat,
		srcBounds:  d.srcBounds,
		scale:      d.scale,
		dst:        d.dst,
		logical:    d.logical,
		state:      d.state,
		text:       d.text != nil,
		unknownSrc: d.src != nil && srcID == 0,
	}
}

func (k textureDensityReplayKey) bounds() image.Rectangle {
	return image.Rectangle{Min: k.dst, Max: k.dst.Add(k.logical)}
}

func resizeTextureDensityU16(buf []uint16, n int) []uint16 {
	if cap(buf) < n {
		return make([]uint16, n)
	}
	return buf[:n]
}

func resizeTextureDensityU32(buf []uint32, n int) []uint32 {
	if cap(buf) < n {
		return make([]uint32, n)
	}
	return buf[:n]
}

func (r *NoxRender) textureDensityBuffer(slot int) *textureDensityBuffers {
	for len(r.textureDensityBuffers) <= slot {
		r.textureDensityBuffers = append(r.textureDensityBuffers, textureDensityBuffers{})
	}
	return &r.textureDensityBuffers[slot]
}

func (r *NoxRender) textureDensityBackground(slot, pixels int) []uint16 {
	if r == nil || pixels <= 0 {
		return nil
	}
	b := r.textureDensityBuffer(slot)
	b.background = resizeTextureDensityU16(b.background, pixels)
	return b.background
}

// textureDensityText pairs the logical font used for layout with a matching
// native 2x bitmap-font sprite. The original face remains authoritative for
// advances, wrapping and hit-testing.
type textureDensityText struct {
	logical font.Face
	native  font.Face
	value   string
	pos     image.Point
	color   Color16
	alpha   uint16
	advance int
}

const (
	textureDensityMaxDraws = 128
	textureDensityMaxBytes = 4 << 20
)

// textureDensityNeedsBackground reports whether a native replay needs the
// logical pixels that existed before the draw. Packed RGB555 sources replay as
// opaque copies, so saving a full-screen menu backdrop is needless allocation
// and can crowd later hover/text records out of the fixed per-frame queue.
func (r *NoxRender) textureDensityNeedsBackground(img *Image) bool {
	if img == nil || len(img.texture16) == 0 || img.textureMat != nil {
		return true
	}
	return r != nil && r.p != nil && (r.p.IsAlphaEnabled() || r.p.Multiply14())
}

func textureDensityRecordBytes(pixels int, hasBackground bool) int {
	// Every record keeps post-draw RGB555 samples and ownership values. The
	// pre-draw samples are retained only for alpha/multiply replay.
	bytes := pixels * (2 + 4)
	if hasBackground {
		bytes += pixels * 2
	}
	return bytes
}

func (r *NoxRender) queueTextureDensity(img *Image, pos image.Point, background []uint16) {
	if r == nil || !r.textureDensityEnabled || r.pix == nil || img == nil || !supportedTextureScale(int(img.textureScale)) || !img.hasNativeTexture() {
		return
	}
	scale := int(img.textureScale)
	if r.texturePresentationScale > 1 && scale != r.texturePresentationScale {
		return
	}
	off, logical, ok := img.Meta()
	if !ok || logical.X <= 0 || logical.Y <= 0 {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectInvalid)
		}
		return
	}
	if !img.nativeTextureValidAtScale(scale) {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectInvalid)
		}
		return
	}
	if r.textureDensityScale != 0 && r.textureDensityScale != scale {
		return
	}
	if r.dword_5d4594_3799484 != 0 || r.interlacing {
		return
	}
	dst := pos.Add(off)
	// A marked source is expected to contain native samples. Do not turn a
	// 1x override into a fake HD draw by upscaling it here.
	srcBounds, ok := img.nativeTextureBounds()
	if !ok {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectInvalid)
		}
		return
	}
	if srcBounds.Dx() < logical.X*scale || srcBounds.Dy() < logical.Y*scale {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectInvalid)
		}
		return
	}
	clip := image.Rectangle{Min: dst, Max: dst.Add(logical)}.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		clip = clip.Intersect(r.p.ClipRect())
	}
	if clip.Empty() {
		return
	}
	pixels := clip.Dx() * clip.Dy()
	hasBackground := len(background) == pixels
	if !hasBackground {
		background = nil
	}
	recordBytes := textureDensityRecordBytes(pixels, hasBackground)
	if len(r.textureDensity) >= textureDensityMaxDraws || r.textureDensityBytes+recordBytes > textureDensityMaxBytes {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectCapacity)
		}
		return
	}
	buffers := r.textureDensityBuffer(len(r.textureDensity))
	buffers.snapshot = resizeTextureDensityU16(buffers.snapshot, pixels)
	buffers.owners = resizeTextureDensityU32(buffers.owners, pixels)
	snapshot := buffers.snapshot
	owners := buffers.owners
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		copy(snapshot[(y-clip.Min.Y)*clip.Dx():], r.pix.Pix[y*r.pix.Stride+clip.Min.X:y*r.pix.Stride+clip.Max.X])
		if len(r.textureOwners) == len(r.pix.Pix) {
			copy(owners[(y-clip.Min.Y)*clip.Dx():], r.textureOwners[y*r.pix.Stride+clip.Min.X:y*r.pix.Stride+clip.Max.X])
		}
	}
	state := RenderData{}
	if r.p != nil {
		state = *r.p
	}
	r.textureDensity = append(r.textureDensity, textureDensityDraw{
		src:        img.texture,
		mat:        img.textureMat,
		src16:      img.texture16,
		srcBounds:  srcBounds,
		scale:      scale,
		dst:        dst,
		logical:    logical,
		snapshot:   snapshot,
		owners:     owners,
		background: background,
		state:      state,
	})
	if r.hdPerf != nil && r.hdPerf.trace {
		r.hdPerf.traceAccepted++
	}
	r.textureDensityScale = scale
	r.textureDensityBytes += recordBytes
}

func (r *NoxRender) textureDensityTextEligible(face font.Face) (font.Face, int, bool) {
	if r == nil || r.pix == nil || !r.textureDensityEnabled || r.dword_5d4594_3799484 != 0 || r.interlacing {
		return nil, 0, false
	}
	// HD font sprites are generated exclusively for the supported 2x mode.
	if r.texturePresentationScale != 2 {
		return nil, 0, false
	}
	native := r.Fonts.NativeFont(face)
	if native == nil {
		return nil, 0, false
	}
	if r.textureDensityScale != 0 && r.textureDensityScale != 2 {
		return nil, 0, false
	}
	return native, 2, true
}

// textureDensityTextSupported is the atomic preflight for one drawString
// operation. Native replay must never silently substitute a '?' for a glyph;
// callers keep the complete logical operation when any rune is unsupported.
func textureDensityTextSupported(native font.Face, value string) bool {
	if native == nil || value == "" {
		return false
	}
	for _, rn := range value {
		// Face.Glyph may legally return a replacement glyph for an unsupported
		// rune. GlyphBounds is the coverage query and does not perform that
		// substitution for the bitmap font faces used by Nox.
		if _, _, ok := native.GlyphBounds(rn); !ok {
			return false
		}
	}
	return true
}

func textureDensityTextColor(c color.Color) (Color16, uint16) {
	if c == nil {
		return Color16{}, 0
	}
	r, g, b, a := c.RGBA()
	return Color16{R: uint16(r >> 8), G: uint16(g >> 8), B: uint16(b >> 8)}, uint16(a >> 8)
}

// markTextureTextOwnership tracks only text pixels that actually changed the
// logical frame. Transparent glyph space must not mask an earlier HD sprite.
func (r *NoxRender) markTextureTextOwnership(clip image.Rectangle, background []uint16) {
	if r == nil || r.pix == nil || len(background) != clip.Dx()*clip.Dy() || clip.Empty() {
		return
	}
	changed := false
	for y := clip.Min.Y; y < clip.Max.Y && !changed; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			si := (y-clip.Min.Y)*clip.Dx() + x - clip.Min.X
			if r.pix.Pix[y*r.pix.Stride+x] != background[si] {
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}
	if len(r.textureOwners) != len(r.pix.Pix) {
		r.textureOwners = make([]uint32, len(r.pix.Pix))
	}
	r.textureOwnerSeq++
	if r.textureOwnerSeq == 0 {
		for i := range r.textureOwners {
			r.textureOwners[i] = 0
		}
		r.textureOwnerSeq = 1
	}
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			si := (y-clip.Min.Y)*clip.Dx() + x - clip.Min.X
			if r.pix.Pix[y*r.pix.Stride+x] != background[si] {
				r.textureOwners[y*r.pix.Stride+x] = r.textureOwnerSeq
			}
		}
	}
}

// queueTextureText records a logical text draw after it reaches PixBuffer.
// Native replay uses the 2x glyph sprites while this snapshot protects later
// logical writes and preserves the game's existing layout calculations.
func (r *NoxRender) queueTextureText(logical, native font.Face, value string, pos image.Point, bounds image.Rectangle, background []uint16, advance int) bool {
	if logical == nil || native == nil || value == "" {
		if r != nil && r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectInvalid)
		}
		return false
	}
	if !textureDensityTextSupported(native, value) {
		if r != nil && r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectMissingGlyph)
		}
		return false
	}
	if r == nil || r.pix == nil || !r.textureDensityEnabled || r.dword_5d4594_3799484 != 0 || r.interlacing || r.texturePresentationScale != 2 {
		return false
	}
	const scale = 2
	if r.textureDensityScale != 0 && r.textureDensityScale != scale {
		return false
	}
	clip := bounds.Intersect(r.pix.Rect)
	if r.p != nil && r.p.Clip() {
		clip = clip.Intersect(r.p.ClipRect())
	}
	if clip.Empty() || len(background) != clip.Dx()*clip.Dy() {
		return false
	}
	pixels := clip.Dx() * clip.Dy()
	recordBytes := pixels * 8
	if len(r.textureDensity) >= textureDensityMaxDraws || r.textureDensityBytes+recordBytes > textureDensityMaxBytes {
		if r.hdPerf != nil {
			r.hdPerf.reject(textureDensityRejectCapacity)
		}
		return false
	}
	r.markTextureTextOwnership(clip, background)
	buffers := r.textureDensityBuffer(len(r.textureDensity))
	buffers.snapshot = resizeTextureDensityU16(buffers.snapshot, pixels)
	buffers.owners = resizeTextureDensityU32(buffers.owners, pixels)
	snapshot := buffers.snapshot
	owners := buffers.owners
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		copy(snapshot[(y-clip.Min.Y)*clip.Dx():], r.pix.Pix[y*r.pix.Stride+clip.Min.X:y*r.pix.Stride+clip.Max.X])
		if len(r.textureOwners) == len(r.pix.Pix) {
			copy(owners[(y-clip.Min.Y)*clip.Dx():], r.textureOwners[y*r.pix.Stride+clip.Min.X:y*r.pix.Stride+clip.Max.X])
		}
	}
	state := RenderData{}
	textColor := color.Color(nil)
	if r.p != nil {
		state = *r.p
		textColor = r.p.TextColor()
	}
	textColor16, textAlpha := textureDensityTextColor(textColor)
	r.textureDensity = append(r.textureDensity, textureDensityDraw{
		scale:      scale,
		dst:        bounds.Min,
		logical:    bounds.Size(),
		snapshot:   snapshot,
		owners:     owners,
		background: background,
		state:      state,
		text: &textureDensityText{
			logical: logical,
			native:  native,
			value:   value,
			pos:     pos,
			color:   textColor16,
			alpha:   textAlpha,
			advance: advance,
		},
	})
	if r.hdPerf != nil && r.hdPerf.trace {
		r.hdPerf.traceAccepted++
	}
	r.textureDensityScale = scale
	r.textureDensityBytes += recordBytes
	return true
}

func (r *NoxRender) resetTextureDensity() {
	for i := range r.textureDensity {
		r.textureDensity[i] = textureDensityDraw{}
	}
	r.textureDensity = r.textureDensity[:0]
	r.textureDensityBytes = 0
	r.textureDensityScale = 0
}

// HasTextureDensityDraws reports whether this frame needs the optional native
// texture presentation path.
func (r *NoxRender) HasTextureDensityDraws() bool {
	return r != nil && len(r.textureDensity) != 0
}

const (
	textureDensityDirtyTileSize   = 16
	textureDensityDirtyFullLimit  = 3 // full redraw at 75% dirty coverage
	textureDensityParallelTiles   = 2
	textureDensityParallelPixels  = 4096
	textureDensityParallelWorkers = 4
)

func (r *NoxRender) texturePresentationFor(base *noximage.Image16, scale int) *noximage.Image16 {
	if base == nil || scale <= 0 {
		return nil
	}
	wantRect := image.Rect(base.Rect.Min.X*scale, base.Rect.Min.Y*scale, base.Rect.Max.X*scale, base.Rect.Max.Y*scale)
	if r.texturePresentation == nil || r.texturePresentation.Rect != wantRect {
		r.texturePresentation = noximage.NewImage16(wantRect)
		r.texturePresentationValid = false
	}
	return r.texturePresentation
}

func (r *NoxRender) scaledTexturePresentationRect(base, out *noximage.Image16, scale int, rc image.Rectangle) {
	if base == nil || out == nil || scale <= 0 {
		return
	}
	rc = rc.Intersect(base.Rect)
	for y := rc.Min.Y; y < rc.Max.Y; y++ {
		for x := rc.Min.X; x < rc.Max.X; x++ {
			v := base.Pix[y*base.Stride+x]
			oy := (y-base.Rect.Min.Y)*scale + out.Rect.Min.Y
			ox := (x-base.Rect.Min.X)*scale + out.Rect.Min.X
			for py := 0; py < scale; py++ {
				for px := 0; px < scale; px++ {
					out.Pix[(oy+py)*out.Stride+ox+px] = v
				}
			}
		}
	}
}

func (r *NoxRender) scaledTexturePresentation(base *noximage.Image16, scale int) *noximage.Image16 {
	out := r.texturePresentationFor(base, scale)
	if out == nil {
		return nil
	}
	r.scaledTexturePresentationRect(base, out, scale, base.Rect)
	return out
}

func (r *NoxRender) clearTextureReplayWritten(out *noximage.Image16) {
	if r == nil || out == nil {
		return
	}
	if len(r.textureReplayWritten) != len(out.Pix) {
		r.textureReplayWritten = make([]bool, len(out.Pix))
		return
	}
	for i := range r.textureReplayWritten {
		r.textureReplayWritten[i] = false
	}
}

func (r *NoxRender) markTexturePresentationDirty(dirty []bool, tilesX int, rc image.Rectangle) {
	if r == nil || r.pix == nil || tilesX <= 0 {
		return
	}
	rc = rc.Intersect(r.pix.Rect)
	if rc.Empty() {
		return
	}
	minX := (rc.Min.X - r.pix.Rect.Min.X) / textureDensityDirtyTileSize
	maxX := (rc.Max.X - 1 - r.pix.Rect.Min.X) / textureDensityDirtyTileSize
	minY := (rc.Min.Y - r.pix.Rect.Min.Y) / textureDensityDirtyTileSize
	maxY := (rc.Max.Y - 1 - r.pix.Rect.Min.Y) / textureDensityDirtyTileSize
	for ty := minY; ty <= maxY; ty++ {
		for tx := minX; tx <= maxX; tx++ {
			ind := ty*tilesX + tx
			if ind >= 0 && ind < len(dirty) {
				dirty[ind] = true
			}
		}
	}
}

// textureDensityDirtyRegions compares the authoritative logical frame and
// stable replay inputs with the last completed presentation. Text is
// deliberately invalidated every frame: it is small, avoids assuming bitmap
// face identity, and preserves all existing text fallback semantics.
func (r *NoxRender) textureDensityDirtyRegions(base *noximage.Image16, scale int) ([]image.Rectangle, bool) {
	if r == nil || base == nil || scale != 2 || !r.texturePresentationValid || r.texturePresentation == nil || r.texturePresentationBaseRect != base.Rect || r.texturePresentationBaseStride != base.Stride || len(r.texturePresentationBase) != len(base.Pix) || len(r.textureOwners) != len(r.texturePresentationOwners) {
		return nil, true
	}
	w, h := base.Rect.Dx(), base.Rect.Dy()
	if w <= 0 || h <= 0 {
		return nil, true
	}
	tilesX := (w + textureDensityDirtyTileSize - 1) / textureDensityDirtyTileSize
	tilesY := (h + textureDensityDirtyTileSize - 1) / textureDensityDirtyTileSize
	tiles := tilesX * tilesY
	if cap(r.texturePresentationDirty) < tiles {
		r.texturePresentationDirty = make([]bool, tiles)
	} else {
		r.texturePresentationDirty = r.texturePresentationDirty[:tiles]
		for i := range r.texturePresentationDirty {
			r.texturePresentationDirty[i] = false
		}
	}
	dirty := r.texturePresentationDirty
	hasOwners := len(r.textureOwners) == len(base.Pix)
	for y := base.Rect.Min.Y; y < base.Rect.Max.Y; y++ {
		for x := base.Rect.Min.X; x < base.Rect.Max.X; x++ {
			ind := y*base.Stride + x
			if base.Pix[ind] != r.texturePresentationBase[ind] || (hasOwners && r.textureOwners[ind] != r.texturePresentationOwners[ind]) {
				tx := (x - base.Rect.Min.X) / textureDensityDirtyTileSize
				ty := (y - base.Rect.Min.Y) / textureDensityDirtyTileSize
				dirty[ty*tilesX+tx] = true
			}
		}
	}
	maxKeys := len(r.texturePresentationKeys)
	if len(r.textureDensity) > maxKeys {
		maxKeys = len(r.textureDensity)
	}
	for i := 0; i < maxKeys; i++ {
		var prev, cur textureDensityReplayKey
		havePrev, haveCur := i < len(r.texturePresentationKeys), i < len(r.textureDensity)
		if havePrev {
			prev = r.texturePresentationKeys[i]
		}
		if haveCur {
			cur = textureDensityReplayKeyOf(r.textureDensity[i])
		}
		changed := !havePrev || !haveCur || prev != cur || prev.text || cur.text || prev.unknownSrc || cur.unknownSrc
		if !changed {
			continue
		}
		if havePrev {
			r.markTexturePresentationDirty(dirty, tilesX, prev.bounds())
		}
		if haveCur {
			r.markTexturePresentationDirty(dirty, tilesX, cur.bounds())
		}
	}
	count := 0
	for _, v := range dirty {
		if v {
			count++
		}
	}
	if count == 0 {
		return nil, false
	}
	if count*4 >= tiles*textureDensityDirtyFullLimit {
		return nil, true
	}
	regions := make([]image.Rectangle, 0, count)
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			if !dirty[ty*tilesX+tx] {
				continue
			}
			x0 := base.Rect.Min.X + tx*textureDensityDirtyTileSize
			y0 := base.Rect.Min.Y + ty*textureDensityDirtyTileSize
			regions = append(regions, image.Rect(x0, y0, minInt(x0+textureDensityDirtyTileSize, base.Rect.Max.X), minInt(y0+textureDensityDirtyTileSize, base.Rect.Max.Y)))
		}
	}
	return regions, false
}

func (r *NoxRender) saveTexturePresentationState(base *noximage.Image16) {
	if r == nil || base == nil {
		return
	}
	if cap(r.texturePresentationBase) < len(base.Pix) {
		r.texturePresentationBase = make([]uint16, len(base.Pix))
	} else {
		r.texturePresentationBase = r.texturePresentationBase[:len(base.Pix)]
	}
	copy(r.texturePresentationBase, base.Pix)
	if len(r.textureOwners) != 0 {
		if cap(r.texturePresentationOwners) < len(r.textureOwners) {
			r.texturePresentationOwners = make([]uint32, len(r.textureOwners))
		} else {
			r.texturePresentationOwners = r.texturePresentationOwners[:len(r.textureOwners)]
		}
		copy(r.texturePresentationOwners, r.textureOwners)
	} else {
		r.texturePresentationOwners = r.texturePresentationOwners[:0]
	}
	r.texturePresentationBaseRect = base.Rect
	r.texturePresentationBaseStride = base.Stride
	if cap(r.texturePresentationKeys) < len(r.textureDensity) {
		r.texturePresentationKeys = make([]textureDensityReplayKey, len(r.textureDensity))
	} else {
		r.texturePresentationKeys = r.texturePresentationKeys[:len(r.textureDensity)]
	}
	for i, d := range r.textureDensity {
		r.texturePresentationKeys[i] = textureDensityReplayKeyOf(d)
	}
	r.texturePresentationValid = true
}

// shouldParallelTextureDensityReplay keeps a one-tile update on one core.
// Worker setup only pays for itself when independent dirty tiles have enough
// native pixels to replay. Text and traces remain serial:
// bitmap faces and trace counters deliberately keep their existing behavior.
func (r *NoxRender) shouldParallelTextureDensityReplay(regions []image.Rectangle) bool {
	if r == nil || len(regions) < textureDensityParallelTiles || len(r.textureDensity) == 0 || (r.hdPerf != nil && r.hdPerf.trace) || runtime.GOMAXPROCS(0) < 2 {
		return false
	}
	pixels := 0
	for _, rc := range regions {
		pixels += rc.Dx() * rc.Dy()
	}
	if pixels*len(r.textureDensity) < textureDensityParallelPixels {
		return false
	}
	for _, d := range r.textureDensity {
		if d.text != nil {
			return false
		}
	}
	return true
}

func (r *NoxRender) replayTextureDensityRegions(dst *noximage.Image16, regions []image.Rectangle) {
	if r == nil || dst == nil || len(regions) == 0 {
		return
	}
	replayRegion := func(rc image.Rectangle) {
		for _, d := range r.textureDensity {
			r.replayTextureDensityRegion(dst, d, rc)
		}
	}
	if !r.shouldParallelTextureDensityReplay(regions) {
		for _, rc := range regions {
			replayRegion(rc)
		}
		return
	}
	workers := minInt(textureDensityParallelWorkers, minInt(runtime.GOMAXPROCS(0), len(regions)))
	jobs := make(chan image.Rectangle, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for rc := range jobs {
				replayRegion(rc)
			}
		}()
	}
	for _, rc := range regions {
		jobs <- rc
	}
	close(jobs)
	wg.Wait()
}

func (r *NoxRender) resizeTexturePresentation(src *noximage.Image16, target image.Point) *noximage.Image16 {
	if src == nil || target.X <= 0 || target.Y <= 0 || src.Size() == target {
		return src
	}
	rect := image.Rect(0, 0, target.X, target.Y)
	if r.textureDisplayPresentation == nil || r.textureDisplayPresentation.Rect != rect {
		r.textureDisplayPresentation = noximage.NewImage16(rect)
	}
	// The game composes its native-density frame first. ApproxBiLinear is used
	// only when that frame does not fit the drawable display size, which keeps
	// the common 4x-to-native path cheap while smoothing menu downscaling.
	xdraw.ApproxBiLinear.Scale(r.textureDisplayPresentation, rect, src, src.Rect, xdraw.Src, nil)
	return r.textureDisplayPresentation
}

// CopyPresentationBuffer keeps the output surface at the requested drawable
// display size. Marked frames are nearest-scaled once and then receive their
// native samples in draw order; unmarked frames still use the same native
// density. The optional target keeps direct renderer tests and callers that do
// not have a display unchanged.
// Queue ownership is reset after the snapshot is consumed, even if no entries
// can be replayed.
func (r *NoxRender) CopyPresentationBuffer(target ...image.Point) *noximage.Image16 {
	if r == nil {
		return nil
	}
	defer r.Bag.endTextureFrame()
	defer r.emitTextureDensityTrace()
	var frameStart, expansionStart, replayStart time.Time
	if r.hdPerf != nil && r.hdPerf.enabled {
		frameStart = time.Now()
	}
	defer func() {
		if !frameStart.IsZero() {
			r.hdPerf.recordFrame(time.Since(frameStart))
		}
	}()
	if r.pix == nil {
		r.texturePresentationValid = false
		r.resetTextureDensity()
		return nil
	}
	base := r.pix
	scale := r.texturePresentationScale
	if scale == 0 {
		scale = r.textureDensityScale
	}
	if !supportedTextureScale(scale) {
		r.texturePresentationValid = false
		r.resetTextureDensity()
		if len(target) != 0 {
			return r.resizeTexturePresentation(base, target[0])
		}
		return base
	}
	regions, full := r.textureDensityDirtyRegions(base, scale)
	if r.hdPerf != nil && r.hdPerf.trace {
		// A trace is a diagnostic oracle: keep its ledger exactly one record per
		// draw rather than splitting it by dirty tile.
		full = true
	}
	var out *noximage.Image16
	if full {
		if !frameStart.IsZero() {
			expansionStart = time.Now()
		}
		out = r.scaledTexturePresentation(base, scale)
		if !expansionStart.IsZero() {
			r.hdPerf.expansionTime += time.Since(expansionStart)
		}
	} else {
		out = r.texturePresentationFor(base, scale)
		if len(regions) != 0 {
			if !frameStart.IsZero() {
				expansionStart = time.Now()
			}
			for _, rc := range regions {
				r.scaledTexturePresentationRect(base, out, scale, rc)
			}
			if !expansionStart.IsZero() {
				r.hdPerf.expansionTime += time.Since(expansionStart)
			}
		}
	}
	if out == nil {
		r.texturePresentationValid = false
		r.resetTextureDensity()
		return nil
	}
	if full || len(regions) != 0 {
		r.clearTextureReplayWritten(out)
		if !frameStart.IsZero() {
			replayStart = time.Now()
		}
		if full {
			for _, d := range r.textureDensity {
				if r.hdPerf != nil && r.hdPerf.trace {
					r.hdPerf.traceCurrent = r.hdPerf.beginTraceRecord(d.text != nil, image.Rectangle{Min: d.dst, Max: d.dst.Add(d.logical)})
				}
				if d.text != nil {
					r.replayTextureText(out, d)
				} else {
					r.replayTextureDensity(out, d)
				}
			}
			if r.hdPerf != nil && r.hdPerf.trace {
				r.hdPerf.traceCurrent = -1
			}
		} else if r.shouldParallelTextureDensityReplay(regions) {
			r.replayTextureDensityRegions(out, regions)
		} else {
			for _, d := range r.textureDensity {
				for _, rc := range regions {
					if d.text != nil {
						r.replayTextureTextRegion(out, d, rc)
					} else {
						r.replayTextureDensityRegion(out, d, rc)
					}
				}
			}
		}
		if !replayStart.IsZero() {
			r.hdPerf.replayTime += time.Since(replayStart)
		}
	}
	r.saveTexturePresentationState(base)
	if len(target) != 0 {
		out = r.resizeTexturePresentation(out, target[0])
	}
	r.resetTextureDensity()
	return out
}

func (r *NoxRender) textureDensityClip(d textureDensityDraw) image.Rectangle {
	if r == nil || r.pix == nil {
		return image.Rectangle{}
	}
	clip := image.Rectangle{Min: d.dst, Max: d.dst.Add(d.logical)}.Intersect(r.pix.Rect)
	if d.state.Clip() {
		clip = clip.Intersect(d.state.ClipRect())
	}
	return clip
}

func (r *NoxRender) textureDensityRecordCurrent(d textureDensityDraw, clip image.Rectangle, x, y int) (int, bool) {
	if r == nil || r.pix == nil || !image.Pt(x, y).In(clip) {
		return 0, false
	}
	si := (y-clip.Min.Y)*clip.Dx() + x - clip.Min.X
	if si < 0 || si >= len(d.snapshot) || r.pix.Pix[y*r.pix.Stride+x] != d.snapshot[si] {
		if r.hdPerf != nil && r.hdPerf.trace {
			r.hdPerf.traceResult(false)
		}
		return si, false
	}
	if len(d.owners) == len(d.snapshot) && len(r.textureOwners) == len(r.pix.Pix) && r.textureOwners[y*r.pix.Stride+x] != d.owners[si] {
		if r.hdPerf != nil && r.hdPerf.trace {
			r.hdPerf.traceResult(false)
		}
		return si, false
	}
	if r.hdPerf != nil && r.hdPerf.trace {
		r.hdPerf.traceResult(true)
	}
	return si, true
}

// replayTextureText restores the original logical background only where the
// text still owns it, then paints the matching HD glyph sprite on top. This is
// intentionally ordered with native sprite draws so labels stay in their
// original z-order without changing any UI coordinates.
func (r *NoxRender) replayTextureText(dst *noximage.Image16, d textureDensityDraw) {
	r.replayTextureTextRegion(dst, d, image.Rectangle{})
}

func (r *NoxRender) replayTextureTextRegion(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle) {
	t := d.text
	if r == nil || dst == nil || t == nil || t.logical == nil || t.native == nil || !supportedTextureScale(d.scale) {
		return
	}
	fullClip := r.textureDensityClip(d)
	clip := fullClip
	if !region.Empty() {
		clip = clip.Intersect(region)
	}
	if clip.Empty() {
		return
	}
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			si, ok := r.textureDensityRecordCurrent(d, fullClip, x, y)
			if !ok {
				continue
			}
			for py := 0; py < d.scale; py++ {
				for px := 0; px < d.scale; px++ {
					dx := (x-r.pix.Rect.Min.X)*d.scale + dst.Rect.Min.X + px
					dy := (y-r.pix.Rect.Min.Y)*d.scale + dst.Rect.Min.Y + py
					if !image.Pt(dx, dy).In(dst.Rect) {
						continue
					}
					di := dy*dst.Stride + dx
					if len(d.background) == len(d.snapshot) && !r.textureReplayWritten[di] {
						dst.Pix[di] = d.background[si]
					}
					r.textureReplayWritten[di] = true
				}
			}
		}
	}

	x := (t.pos.X-r.pix.Rect.Min.X)*d.scale + dst.Rect.Min.X
	baseline := (t.pos.Y-r.pix.Rect.Min.Y)*d.scale + dst.Rect.Min.Y + t.native.Metrics().CapHeight.Round()
	for _, rn := range t.value {
		dr, mask, maskp, _, ok := t.native.Glyph(fixed.P(x, baseline), rn)
		if !ok {
			dr, mask, maskp, _, ok = t.native.Glyph(fixed.P(x, baseline), '?')
		}
		if ok {
			for y := dr.Min.Y; y < dr.Max.Y; y++ {
				if y < dst.Rect.Min.Y || y >= dst.Rect.Max.Y {
					continue
				}
				ly := (y-dst.Rect.Min.Y)/d.scale + r.pix.Rect.Min.Y
				for xx := dr.Min.X; xx < dr.Max.X; xx++ {
					if xx < dst.Rect.Min.X || xx >= dst.Rect.Max.X {
						continue
					}
					lx := (xx-dst.Rect.Min.X)/d.scale + r.pix.Rect.Min.X
					if !region.Empty() && !image.Pt(lx, ly).In(region) {
						continue
					}
					_, visible := r.textureDensityRecordCurrent(d, fullClip, lx, ly)
					if !visible {
						continue
					}
					a := color.AlphaModel.Convert(mask.At(maskp.X+xx-dr.Min.X, maskp.Y+y-dr.Min.Y)).(color.Alpha).A
					if a == 0 || t.alpha == 0 {
						continue
					}
					alpha := uint16(a) * t.alpha / 255
					di := y*dst.Stride + xx
					dst.Pix[di] = blendTexturePixel(dst.Pix[di], t.color, alpha)
				}
			}
		}
		advance, ok := t.logical.GlyphAdvance(rn)
		if !ok {
			advance, _ = t.logical.GlyphAdvance('?')
		}
		x += (advance.Round() + t.advance) * d.scale
	}
}

func (r *NoxRender) replayTextureDensity(dst *noximage.Image16, d textureDensityDraw) {
	r.replayTextureDensityRegion(dst, d, image.Rectangle{})
}

func (r *NoxRender) replayTextureDensityRegion(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle) {
	if r.replayTextureDensityPackedRegion(dst, d, region) {
		return
	}
	if r.replayTextureDensityNRGBARegion(dst, d, region) {
		return
	}
	if !supportedTextureScale(d.scale) {
		return
	}
	fullClip := r.textureDensityClip(d)
	clip := fullClip
	if !region.Empty() {
		clip = clip.Intersect(region)
	}
	if clip.Empty() {
		return
	}
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			// A later logical draw owns this location. Keep the base frame
			// there and mask the HD replay, even when it drew the same color.
			si := (y-fullClip.Min.Y)*fullClip.Dx() + x - fullClip.Min.X
			if y-d.dst.Y >= 0 && y-d.dst.Y < d.logical.Y && x-d.dst.X >= 0 && x-d.dst.X < d.logical.X {
				// snapshot is clipped to the queue record's visible rect. The
				// record currently uses the same intersection as this replay.
				if si >= 0 && si < len(d.snapshot) && (r.pix.Pix[y*r.pix.Stride+x] != d.snapshot[si] || (len(d.owners) == len(d.snapshot) && len(r.textureOwners) == len(r.pix.Pix) && r.textureOwners[y*r.pix.Stride+x] != d.owners[si])) {
					continue
				}
			}
			lx, ly := x-d.dst.X, y-d.dst.Y
			for py := 0; py < d.scale; py++ {
				for px := 0; px < d.scale; px++ {
					sx := d.srcBounds.Min.X + lx*d.scale + px
					sy := d.srcBounds.Min.Y + ly*d.scale + py
					if sx < d.srcBounds.Min.X || sy < d.srcBounds.Min.Y || sx >= d.srcBounds.Max.X || sy >= d.srcBounds.Max.Y {
						continue
					}
					sc, alpha, ok := d.sampleColor(sx, sy)
					if !ok {
						continue
					}
					dx := (x-r.pix.Rect.Min.X)*d.scale + dst.Rect.Min.X + px
					dy := (y-r.pix.Rect.Min.Y)*d.scale + dst.Rect.Min.Y + py
					di := dy*dst.Stride + dx
					if len(d.background) == len(d.snapshot) && !r.textureReplayWritten[di] {
						dst.Pix[di] = d.background[si]
					}
					r.textureReplayWritten[di] = true
					dst.Pix[di] = blendTexturePixel(dst.Pix[di], sc, alpha)
				}
			}
		}
	}
}

// replayTextureDensityPacked copies preconverted opaque native rows. Most
// menu and world backgrounds use this path, avoiding per-frame RGBA decoding
// and per-subpixel ownership checks.
func (r *NoxRender) replayTextureDensityPacked(dst *noximage.Image16, d textureDensityDraw) bool {
	return r.replayTextureDensityPackedRegion(dst, d, image.Rectangle{})
}

func (r *NoxRender) replayTextureDensityPackedRegion(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle) bool {
	if r == nil || dst == nil || len(d.src16) == 0 || d.mat != nil || d.state.IsAlphaEnabled() || d.state.Multiply14() || !supportedTextureScale(d.scale) {
		return false
	}
	fullClip := r.textureDensityClip(d)
	clip := fullClip
	if !region.Empty() {
		clip = clip.Intersect(region)
	}
	if clip.Empty() {
		return true
	}
	srcW, srcH := d.srcBounds.Dx(), d.srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 || len(d.src16) != srcW*srcH || srcW < d.logical.X*d.scale || srcH < d.logical.Y*d.scale {
		return false
	}
	hasOwners := len(d.owners) == len(d.snapshot) && len(r.textureOwners) == len(r.pix.Pix)
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		x := clip.Min.X
		for x < clip.Max.X {
			si := (y-fullClip.Min.Y)*fullClip.Dx() + x - fullClip.Min.X
			current := si >= 0 && si < len(d.snapshot) && r.pix.Pix[y*r.pix.Stride+x] == d.snapshot[si] && (!hasOwners || r.textureOwners[y*r.pix.Stride+x] == d.owners[si])
			if !current {
				x++
				continue
			}
			start := x
			for x < clip.Max.X {
				si = (y-fullClip.Min.Y)*fullClip.Dx() + x - fullClip.Min.X
				if si < 0 || si >= len(d.snapshot) || r.pix.Pix[y*r.pix.Stride+x] != d.snapshot[si] || (hasOwners && r.textureOwners[y*r.pix.Stride+x] != d.owners[si]) {
					break
				}
				x++
			}
			count := (x - start) * d.scale
			srcX := (start - d.dst.X) * d.scale
			srcY := (y - d.dst.Y) * d.scale
			dstX := (start-r.pix.Rect.Min.X)*d.scale + dst.Rect.Min.X
			dstY := (y-r.pix.Rect.Min.Y)*d.scale + dst.Rect.Min.Y
			for py := 0; py < d.scale; py++ {
				srcOff := (srcY+py)*srcW + srcX
				dstOff := (dstY+py)*dst.Stride + dstX
				copy(dst.Pix[dstOff:dstOff+count], d.src16[srcOff:srcOff+count])
				for i := 0; i < count; i++ {
					r.textureReplayWritten[dstOff+i] = true
				}
			}
		}
	}
	return true
}

// replayTextureDensityNRGBA handles the common PNG path without image.Image
// conversions. It retains the material, multiply, and alpha rules from
// sampleColor while avoiding At plus NRGBAModel.Convert for every subpixel.
func (r *NoxRender) replayTextureDensityNRGBA(dst *noximage.Image16, d textureDensityDraw) bool {
	return r.replayTextureDensityNRGBARegion(dst, d, image.Rectangle{})
}

func (r *NoxRender) replayTextureDensityNRGBARegion(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle) bool {
	src, ok := d.src.(*image.NRGBA)
	if !ok || !supportedTextureScale(d.scale) {
		return false
	}
	if d.scale == 2 && d.mat == nil && !d.state.IsAlphaEnabled() && !d.state.Multiply14() && d.srcBounds.In(src.Rect) && d.srcBounds.Dx() >= d.logical.X*2 && d.srcBounds.Dy() >= d.logical.Y*2 {
		return r.replayTextureDensityNRGBA2x(dst, d, region, src)
	}
	return r.replayTextureDensityNRGBAGeneric(dst, d, region, src)
}

func (r *NoxRender) replayTextureDensityNRGBAGeneric(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle, src *image.NRGBA) bool {
	fullClip := r.textureDensityClip(d)
	clip := fullClip
	if !region.Empty() {
		clip = clip.Intersect(region)
	}
	if clip.Empty() {
		return true
	}
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			si := (y-fullClip.Min.Y)*fullClip.Dx() + x - fullClip.Min.X
			if y-d.dst.Y < 0 || y-d.dst.Y >= d.logical.Y || x-d.dst.X < 0 || x-d.dst.X >= d.logical.X {
				continue
			}
			if si >= len(d.snapshot) || (r.pix.Pix[y*r.pix.Stride+x] != d.snapshot[si] || (len(d.owners) == len(d.snapshot) && len(r.textureOwners) == len(r.pix.Pix) && r.textureOwners[y*r.pix.Stride+x] != d.owners[si])) {
				continue
			}
			lx, ly := x-d.dst.X, y-d.dst.Y
			for py := 0; py < d.scale; py++ {
				for px := 0; px < d.scale; px++ {
					sx := d.srcBounds.Min.X + lx*d.scale + px
					sy := d.srcBounds.Min.Y + ly*d.scale + py
					if sx < d.srcBounds.Min.X || sy < d.srcBounds.Min.Y || sx >= d.srcBounds.Max.X || sy >= d.srcBounds.Max.Y {
						continue
					}
					at := src.PixOffset(sx, sy)
					alpha := uint16(src.Pix[at+3])
					c := Color16{R: uint16(src.Pix[at]), G: uint16(src.Pix[at+1]), B: uint16(src.Pix[at+2])}
					if d.mat != nil && image.Pt(sx, sy).In(d.mat.Bounds()) {
						if ind := d.mat.Pix[d.mat.PixOffset(sx, sy)]; ind != 0 && int(ind)-1 < len(d.state.materials) {
							m := d.state.materials[int(ind)-1].Color
							intensity := c.R
							c.R = uint16((uint32(m.R) * uint32(intensity)) >> 8)
							c.G = uint16((uint32(m.G) * uint32(intensity)) >> 8)
							c.B = uint16((uint32(m.B) * uint32(intensity)) >> 8)
						}
					}
					if d.state.Multiply14() {
						c = c.Mult(d.state.ColorMultA())
					}
					if d.state.IsAlphaEnabled() {
						alpha = alpha * uint16(d.state.Alpha()) / 255
					}
					dx := (x-r.pix.Rect.Min.X)*d.scale + dst.Rect.Min.X + px
					dy := (y-r.pix.Rect.Min.Y)*d.scale + dst.Rect.Min.Y + py
					di := dy*dst.Stride + dx
					if len(d.background) == len(d.snapshot) && !r.textureReplayWritten[di] {
						dst.Pix[di] = d.background[si]
					}
					r.textureReplayWritten[di] = true
					dst.Pix[di] = blendTexturePixel(dst.Pix[di], c, alpha)
				}
			}
		}
	}
	return true
}

// replayTextureDensityNRGBA2x is the common menu path: an unmaterialed 2x
// PNG with source alpha. All clip/source validation is done once above; this
// keeps the per-pixel loop to ownership, alpha, and four output samples.
func (r *NoxRender) replayTextureDensityNRGBA2x(dst *noximage.Image16, d textureDensityDraw, region image.Rectangle, src *image.NRGBA) bool {
	fullClip := r.textureDensityClip(d)
	clip := fullClip
	if !region.Empty() {
		clip = clip.Intersect(region)
	}
	if clip.Empty() {
		return true
	}
	hasOwners := len(d.owners) == len(d.snapshot) && len(r.textureOwners) == len(r.pix.Pix)
	hasBackground := len(d.background) == len(d.snapshot)
	fullW := fullClip.Dx()
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		siRow := (y - fullClip.Min.Y) * fullW
		srcY := d.srcBounds.Min.Y + (y-d.dst.Y)*2
		dstY := (y-r.pix.Rect.Min.Y)*2 + dst.Rect.Min.Y
		for x := clip.Min.X; x < clip.Max.X; x++ {
			si := siRow + x - fullClip.Min.X
			if si < 0 || si >= len(d.snapshot) || r.pix.Pix[y*r.pix.Stride+x] != d.snapshot[si] || (hasOwners && r.textureOwners[y*r.pix.Stride+x] != d.owners[si]) {
				if r.hdPerf != nil && r.hdPerf.trace {
					r.hdPerf.traceResult(false)
				}
				continue
			}
			if r.hdPerf != nil && r.hdPerf.trace {
				r.hdPerf.traceResult(true)
			}
			srcX := d.srcBounds.Min.X + (x-d.dst.X)*2
			srcAt := src.PixOffset(srcX, srcY)
			dstX := (x-r.pix.Rect.Min.X)*2 + dst.Rect.Min.X
			for py := 0; py < 2; py++ {
				srcRow := srcAt + py*src.Stride
				dstRow := (dstY+py)*dst.Stride + dstX
				for px := 0; px < 2; px++ {
					at := srcRow + px*4
					di := dstRow + px
					if hasBackground && !r.textureReplayWritten[di] {
						dst.Pix[di] = d.background[si]
					}
					r.textureReplayWritten[di] = true
					alpha := uint16(src.Pix[at+3])
					if alpha == 0 {
						continue
					}
					c := Color16{R: uint16(src.Pix[at]), G: uint16(src.Pix[at+1]), B: uint16(src.Pix[at+2])}
					if alpha == 255 {
						dst.Pix[di] = c.Make16()
					} else {
						dst.Pix[di] = blendTexturePixel(dst.Pix[di], c, alpha)
					}
				}
			}
		}
	}
	return true
}

func (d textureDensityDraw) sampleColor(x, y int) (Color16, uint16, bool) {
	var n color.NRGBA
	if len(d.src16) != 0 {
		if !image.Pt(x, y).In(d.srcBounds) {
			return Color16{}, 0, false
		}
		ind := (y-d.srcBounds.Min.Y)*d.srcBounds.Dx() + x - d.srcBounds.Min.X
		if ind < 0 || ind >= len(d.src16) {
			return Color16{}, 0, false
		}
		c := SplitColor16(d.src16[ind])
		n = color.NRGBA{R: uint8(c.R), G: uint8(c.G), B: uint8(c.B), A: 0xff}
	} else {
		if d.src == nil {
			return Color16{}, 0, false
		}
		n = color.NRGBAModel.Convert(d.src.At(x, y)).(color.NRGBA)
		if n.A == 0 {
			return Color16{}, 0, true
		}
	}
	if d.mat != nil && x >= d.mat.Bounds().Min.X && y >= d.mat.Bounds().Min.Y && x < d.mat.Bounds().Max.X && y < d.mat.Bounds().Max.Y {
		if ind := d.mat.ColorIndexAt(x, y); ind != 0 && int(ind)-1 < len(d.state.materials) {
			// PCX decoding shifts material slots by +1 so mask index zero can
			// mean "ordinary RGBA pixel". The source RGB is the material's
			// grayscale intensity, not a replacement color.
			m := d.state.materials[int(ind)-1].Color
			intensity := uint16(n.R)
			n.R = uint8((uint32(m.R) * uint32(intensity)) >> 8)
			n.G = uint8((uint32(m.G) * uint32(intensity)) >> 8)
			n.B = uint8((uint32(m.B) * uint32(intensity)) >> 8)
		}
	}
	c := Color16{R: uint16(n.R), G: uint16(n.G), B: uint16(n.B)}
	if d.state.Multiply14() {
		c = c.Mult(d.state.ColorMultA())
	}
	alpha := uint16(n.A)
	if d.state.IsAlphaEnabled() {
		alpha = alpha * uint16(d.state.Alpha()) / 255
	}
	return c, alpha, true
}

func blendTexturePixel(dst uint16, src Color16, alpha uint16) uint16 {
	if alpha == 0 {
		return dst
	}
	if alpha >= 255 {
		return src.Make16()
	}
	return src.OverAlpha(255-alpha, SplitColor16(dst)).Make16()
}
