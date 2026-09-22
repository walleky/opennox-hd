package noxrender

import (
	"archive/zip"
	"container/heap"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/opennox/libs/bag"
	"github.com/opennox/libs/datapath"
	"github.com/opennox/libs/log"
	"github.com/opennox/libs/noximage/pcx"
	"github.com/opennox/libs/things"

	"github.com/opennox/opennox/v1/legacy/common/alloc"
	"github.com/opennox/opennox/v1/legacy/common/alloc/handles"
)

var (
	debugBagImages = os.Getenv("NOX_DEBUG_BAG_IMAGES") == "true"
	imgLog         = log.New("images")
)

type ImageHandle unsafe.Pointer

type Image struct {
	c        *RenderSprites
	h        ImageHandle
	typ      int
	bag      *bag.ImageRec
	raw      []byte
	override []byte
	// texture is retained only for an opt-in native texture_scale override. The
	// regular PCX path remains the source of truth for the logical draw.
	texture       image.Image
	textureMat    *image.Paletted
	texture16     []uint16
	textureBounds image.Rectangle
	textureScale  uint8
	textureBytes  int64
	textureUsed   uint64
	textureLRU    *textureLRUEntry
	nocgo         bool
	cdata         []byte
	cfree         func()
	Field_1_0     uint16
	Field_1_1     uint16
}

func (img *Image) C() ImageHandle {
	if img == nil {
		return nil
	}
	if img.nocgo {
		panic("image not allowed in cgo context")
	}
	if img.h == nil {
		img.h = ImageHandle(handles.NewPtr())
		img.c.byHandle[img.h] = img
	}
	return img.h
}

func (img *Image) Free() {
	if img == nil {
		return
	}
	img.cdata = nil
	img.releaseNativeTexture()
	img.textureScale = 0
	if img.cfree != nil {
		img.cfree()
	}
	img.cfree = nil
}

// packOpaqueTexture converts the common fully opaque NRGBA source once at
// load time. The renderer presents RGB5551 pixels, so this is lossless for
// the normal opaque path and halves the retained cache footprint.
func packOpaqueTexture(src image.Image, mat *image.Paletted) ([]uint16, image.Rectangle, bool) {
	if mat != nil {
		return nil, image.Rectangle{}, false
	}
	pix, ok := src.(*image.NRGBA)
	if !ok {
		return nil, image.Rectangle{}, false
	}
	bounds := pix.Bounds()
	if bounds.Empty() {
		return nil, image.Rectangle{}, false
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for at, end := pix.PixOffset(bounds.Min.X, y), pix.PixOffset(bounds.Max.X, y); at < end; at += 4 {
			if pix.Pix[at+3] != 0xff {
				return nil, image.Rectangle{}, false
			}
		}
	}
	out := make([]uint16, bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for at, end, ind := pix.PixOffset(bounds.Min.X, y), pix.PixOffset(bounds.Max.X, y), (y-bounds.Min.Y)*bounds.Dx(); at < end; at, ind = at+4, ind+1 {
			out[ind] = Color16{R: uint16(pix.Pix[at]), G: uint16(pix.Pix[at+1]), B: uint16(pix.Pix[at+2])}.Make16()
		}
	}
	return out, bounds, true
}

func nativeTextureBytes(src image.Image, mat *image.Paletted, packed []uint16) int64 {
	if len(packed) != 0 {
		return int64(len(packed)) * 2
	}
	var n int64
	switch v := src.(type) {
	case *image.NRGBA:
		n += int64(len(v.Pix))
	case *image.RGBA:
		n += int64(len(v.Pix))
	case *image.Paletted:
		n += int64(len(v.Pix))
	default:
		if src != nil {
			b := src.Bounds()
			n += int64(b.Dx()) * int64(b.Dy()) * 4
		}
	}
	if mat != nil {
		n += int64(len(mat.Pix))
	}
	return n
}

func (img *Image) retainNativeTexture(src image.Image, mat *image.Paletted) {
	img.releaseNativeTexture()
	if packed, bounds, ok := packOpaqueTexture(src, mat); ok {
		img.texture16 = packed
		img.textureBounds = bounds
	} else {
		img.texture = src
		img.textureMat = mat
	}
	img.textureBytes = nativeTextureBytes(img.texture, img.textureMat, img.texture16)
	if img.c != nil {
		img.textureUsed = img.c.textureEpoch
		img.c.textureBytes += img.textureBytes
		img.c.trackNativeTexture(img)
	}
}

// releaseNativeTexture drops only the large decoded native source. The logical
// PCX override and its C-visible allocation deliberately remain stable.
func (img *Image) releaseNativeTexture() {
	if img == nil {
		return
	}
	if img.c != nil {
		img.c.untrackNativeTexture(img)
		img.c.textureBytes -= img.textureBytes
		if img.c.textureBytes < 0 {
			img.c.textureBytes = 0
		}
	}
	img.texture = nil
	img.textureMat = nil
	img.texture16 = nil
	img.textureBounds = image.Rectangle{}
	img.textureBytes = 0
}

func (img *Image) hasNativeTexture() bool {
	return img != nil && (img.texture != nil || len(img.texture16) != 0)
}

func (img *Image) nativeTextureBounds() (image.Rectangle, bool) {
	if img == nil {
		return image.Rectangle{}, false
	}
	if len(img.texture16) != 0 {
		return img.textureBounds, !img.textureBounds.Empty()
	}
	if img.texture != nil {
		return img.texture.Bounds(), true
	}
	return image.Rectangle{}, false
}

func (img *Image) nativeTextureValidAtScale(scale int) bool {
	bounds, ok := img.nativeTextureBounds()
	if !ok || !supportedTextureScale(scale) || bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx()%scale != 0 || bounds.Dy()%scale != 0 {
		return false
	}
	if len(img.texture16) != 0 {
		return img.textureMat == nil && len(img.texture16) == bounds.Dx()*bounds.Dy()
	}
	return textureSourceValidAtScale(img.texture, img.textureMat, scale)
}

func (img *Image) ensureNativeTexture() bool {
	if img == nil || !supportedTextureScale(int(img.textureScale)) {
		return false
	}
	if !img.hasNativeTexture() {
		if img.c != nil && img.c.hdPerf != nil {
			img.c.hdPerf.decodeMisses++
		}
		var started time.Time
		if img.c != nil && img.c.hdPerf != nil && img.c.hdPerf.enabled {
			started = time.Now()
		}
		if img.c == nil || img.bag == nil {
			if img.c != nil && img.c.hdPerf != nil {
				img.c.hdPerf.decodeFailures++
			}
			return false
		}
		sect := int(img.bag.SegmInd)
		offs := int(img.bag.Offset)
		im, scale, err := img.c.imageByBagSection(sect, offs)
		if err != nil {
			if img.c.hdPerf != nil {
				img.c.hdPerf.decodeFailures++
			}
			img.c.log.Error("cannot reload texture-density sprite", "err", err)
			if !started.IsZero() {
				img.c.hdPerf.decodeTime += time.Since(started)
			}
			return false
		}
		if im == nil || scale != int(img.textureScale) || !textureSourceValidAtScale(im.Image, im.Material, scale) {
			if img.c.hdPerf != nil {
				img.c.hdPerf.decodeFailures++
			}
			if !started.IsZero() {
				img.c.hdPerf.decodeTime += time.Since(started)
			}
			return false
		}
		img.retainNativeTexture(im.Image, im.Material)
		if !started.IsZero() {
			img.c.hdPerf.decodeTime += time.Since(started)
		}
	}
	if img.c != nil {
		img.textureUsed = img.c.textureEpoch
		img.c.touchNativeTexture(img)
	}
	return true
}

func (img *Image) String() string {
	if img == nil {
		return "<nil>"
	}
	if img.override != nil {
		return fmt.Sprintf("{type=%d, override=[%d]}", img.Type(), len(img.override))
	}
	if img.bag != nil {
		return fmt.Sprintf("{type=%d, idx=%d, data=[%d]}", img.Type(), img.bag.Index, len(img.raw))
	}
	return fmt.Sprintf("{type=%d, raw=[%d]}", img.Type(), len(img.raw))
}

func (img *Image) Type() int {
	if img == nil {
		return -1
	}
	if img.bag != nil {
		return int(img.bag.Type)
	}
	return img.typ
}

func (img *Image) loadOverride() []byte {
	if img == nil || img.raw != nil {
		return nil
	}
	switch img.Type() {
	default:
		return nil
	case 3, 4, 5, 6:
	}
	if img.override != nil {
		return img.override
	}
	sect := int(img.bag.SegmInd)
	offs := int(img.bag.Offset)

	im, scale, err := img.c.imageByBagSection(sect, offs)
	if err != nil {
		img.c.log.Error("cannot load sprite", "err", err)
		return nil
	} else if im == nil {
		return nil
	}
	if supportedTextureScale(scale) {
		if !textureSourceValidAtScale(im.Image, im.Material, scale) {
			// A native dimension that is not divisible by the declared scale
			// footprint. Preserve the original BAG logical draw instead of
			// replacing it with a physically larger override.
			img.releaseNativeTexture()
			img.textureScale = 0
			return nil
		}
		img.textureScale = uint8(scale)
		img.retainNativeTexture(im.Image, im.Material)
		// Keep the original BAG sprite as the logical ownership/base pass.
		// Besides being exact, it avoids round-tripping PCX material masks:
		// decoded masks store engine material slots shifted by +1 so that
		// zero can mean "not a material pixel". The retained native source
		// is replayed over this base at presentation time.
		return nil
	}
	img.override = pcx.Encode(im)
	return img.override
}

func textureLogicalSize(src image.Image, scale int) (image.Point, bool) {
	if src == nil || scale <= 0 {
		return image.Point{}, false
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx()%scale != 0 || b.Dy()%scale != 0 {
		return image.Point{}, false
	}
	return image.Pt(b.Dx()/scale, b.Dy()/scale), true
}

func supportedTextureScale(scale int) bool {
	return scale == 2 || scale == 4
}

func textureSourceValid(src image.Image, mat *image.Paletted) bool {
	return textureSourceValidAtScale(src, mat, 2)
}

func textureSourceValidAtScale(src image.Image, mat *image.Paletted, scale int) bool {
	if !supportedTextureScale(scale) {
		return false
	}
	if _, ok := textureLogicalSize(src, scale); !ok {
		return false
	}
	return mat == nil || mat.Bounds() == src.Bounds()
}

func (img *Image) Pixdata() []byte {
	if img == nil {
		return nil
	}
	if img.cdata != nil {
		return img.cdata
	}
	data := img.loadOverride()
	if data == nil {
		data = img.bagPixdata()
	}
	if len(data) == 0 {
		panic("cannot load")
	}
	// TODO: remove interning when we get rid of C renderer
	img.cdata, img.cfree = alloc.CloneSlice(data)
	return img.cdata
}

func (img *Image) bagPixdata() []byte { // nox_video_getImagePixdata_42FB30
	if img == nil {
		return nil
	}
	if img.Type()&0x3F == 7 {
		return nil
	}
	if img.Type()&0x80 != 0 {
		panic("unreachable")
	}
	if img.raw != nil {
		return img.raw
	}
	data, err := img.bag.Raw()
	if err != nil {
		panic(err)
	}
	img.raw = data
	return data
}

func (img *Image) Meta() (off, sz image.Point, ok bool) {
	pix := img.Pixdata()
	if len(pix) < 8 {
		ok = false
		return
	}
	sz.X = int(int32(binary.LittleEndian.Uint32(pix[0:])))
	sz.Y = int(int32(binary.LittleEndian.Uint32(pix[4:])))
	if len(pix) < 16 {
		ok = false
		return
	}
	ok = true
	off.X = int(int32(binary.LittleEndian.Uint32(pix[8:])))
	off.Y = int(int32(binary.LittleEndian.Uint32(pix[12:])))
	return
}

func (b *RenderSprites) readVideobag(path string) error {
	f, err := bag.Open(path)
	if err != nil {
		return err
	}
	imgs, err := f.Images()
	if err != nil {
		_ = f.Close()
		return err
	}
	b.bag = f
	b.byIndex = make([]*Image, 0, len(imgs))
	for _, img := range imgs {
		b.byIndex = append(b.byIndex, &Image{c: b, bag: img})
	}
	return nil
}

type bagImage struct {
	*bag.ImageRec
	Meta *pcx.ImageMeta
}

func (b *RenderSprites) ReadVideoBag() error {
	return b.readVideobag("video.bag")
}

type RenderSprites struct {
	log      *slog.Logger
	bag      *bag.File
	byHandle map[ImageHandle]*Image
	byIndex  []*Image

	once   sync.Once
	err    error
	bySect map[int]map[int]*bagImage
	seen   map[*bagImage]struct{}
	zip    *zip.ReadCloser

	textureEpoch  uint64
	textureBytes  int64
	textureBudget int64
	textureLRU    textureLRUHeap
	hdPerf        *textureDensityPerf
}

// textureLRUEntry is kept exactly once for every retained native source. The
// index makes usage updates and removal bounded, without stale heap entries.
type textureLRUEntry struct {
	img   *Image
	index int
}

type textureLRUHeap []*textureLRUEntry

func (h textureLRUHeap) Len() int { return len(h) }
func (h textureLRUHeap) Less(i, j int) bool {
	return h[i].img.textureUsed < h[j].img.textureUsed
}
func (h textureLRUHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *textureLRUHeap) Push(v any) {
	e := v.(*textureLRUEntry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *textureLRUHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.index = -1
	*h = old[:n-1]
	return e
}

func (b *RenderSprites) trackNativeTexture(img *Image) {
	if b == nil || img == nil || !img.hasNativeTexture() || img.textureLRU != nil {
		return
	}
	e := &textureLRUEntry{img: img, index: -1}
	img.textureLRU = e
	heap.Push(&b.textureLRU, e)
}

func (b *RenderSprites) untrackNativeTexture(img *Image) {
	if b == nil || img == nil || img.textureLRU == nil {
		return
	}
	e := img.textureLRU
	img.textureLRU = nil
	if e.index >= 0 && e.index < len(b.textureLRU) && b.textureLRU[e.index] == e {
		heap.Remove(&b.textureLRU, e.index)
	}
}

func (b *RenderSprites) touchNativeTexture(img *Image) {
	if b == nil || img == nil || img.textureLRU == nil {
		return
	}
	heap.Fix(&b.textureLRU, img.textureLRU.index)
}

func (b *RenderSprites) init(log *slog.Logger) {
	b.log = log
	b.byHandle = make(map[ImageHandle]*Image)
	b.textureEpoch = 1
	b.textureBudget = parseTextureCacheBudget(os.Getenv("NOX_TEXTURE_CACHE_MB"))
}

const defaultTextureCacheBudget = int64(256 << 20)

func parseTextureCacheBudget(value string) int64 {
	if value == "" {
		return defaultTextureCacheBudget
	}
	mb, err := strconv.ParseInt(value, 10, 64)
	if err != nil || mb < 32 || mb > 1024 {
		return defaultTextureCacheBudget
	}
	return mb << 20
}

// endTextureFrame runs after presentation, when no replay record or C draw is
// still reading a native source. It evicts least-recently-used decoded 2x
// images until the configured byte budget is met. Images used in the frame
// being presented are pinned until at least the next frame.
func (b *RenderSprites) endTextureFrame() {
	if b == nil {
		return
	}
	if b.textureEpoch == 0 {
		b.textureEpoch = 1
	}
	if b.textureBudget > 0 && b.textureBytes > b.textureBudget {
		for b.textureBytes > b.textureBudget && len(b.textureLRU) > 0 {
			e := b.textureLRU[0]
			if e.img.textureUsed >= b.textureEpoch {
				break // Heap contains only current-frame entries from here on.
			}
			e.img.releaseNativeTexture()
			if b.hdPerf != nil {
				b.hdPerf.cacheEvictions++
			}
		}
	}
	if b.hdPerf != nil {
		b.hdPerf.cacheBytes = b.textureBytes
		if b.textureBytes > b.hdPerf.cacheHighWater {
			b.hdPerf.cacheHighWater = b.textureBytes
		}
	}
	b.textureEpoch++
	if b.textureEpoch == 0 {
		b.textureEpoch = 1
	}
}

func (b *RenderSprites) Free() {
	if b.bag != nil {
		_ = b.bag.Close()
	}
	b.bag = nil
	for _, img := range b.byIndex {
		img.Free()
	}
	if b.zip != nil {
		_ = b.zip.Close()
		b.zip = nil
	}
	b.byIndex = nil
	b.textureLRU = nil
	b.byHandle = make(map[ImageHandle]*Image)
}

func NewRawImage(typ int, data []byte) *Image {
	return &Image{typ: typ, raw: data, nocgo: true}
}

func (b *RenderSprites) AsImage(p ImageHandle) *Image {
	if p == nil {
		return nil
	}
	img := b.byHandle[p]
	if img == nil {
		err := fmt.Errorf("unexpected image handle: %x", p)
		imgLog.Printf("%v", err)
	}
	return img
}

func (b *RenderSprites) ImageByIndex(ind int) *Image {
	return b.byIndex[ind]
}

func (b *RenderSprites) ThingsImageRef(ref *things.ImageRef) *Image {
	if ref == nil {
		return nil
	}
	return b.ImageRef(ref.Ind, byte(ref.Ind2), ref.Name)
}

func (b *RenderSprites) ImageRef(ind int, typ byte, name2 string) *Image {
	if ind != -1 {
		return b.ImageByIndex(ind)
	}
	log.Printf("ImageRef(%d, %d, %q)", ind, int(typ), name2)
	return b.LoadExternalImage(typ, name2)
}

func (b *RenderSprites) LoadExternalImage(typ byte, name string) *Image {
	// TODO: this one is supposed to load PCX images from FS
	//panic("TODO: read PCX from FS")
	return nil
}

func (b *RenderSprites) loadAndIndexVideoBag() error {
	f, err := bag.Open(datapath.Data("video.bag"))
	if err != nil {
		return fmt.Errorf("error reading video bag: %w", err)
	}
	// we don't close it because we will load fallback metadata from it
	//defer f.Close()
	sects, err := f.Segments()
	if err != nil {
		return fmt.Errorf("error reading video bag sections: %w", err)
	}
	b.seen = make(map[*bagImage]struct{})
	b.bySect = make(map[int]map[int]*bagImage)
	for _, s := range sects {
		byOff := make(map[int]*bagImage)
		b.bySect[s.Index] = byOff
		for _, img := range s.Images {
			bi := &bagImage{ImageRec: img}
			byOff[int(img.Offset)] = bi
		}
	}
	if err := b.openVideoZip(); err != nil {
		return err
	}
	return nil
}

func (b *RenderSprites) openVideoZip() error {
	zf, err := zip.OpenReader(datapath.Data("video.bag.zip"))
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	b.zip = zf
	return nil
}

func (b *RenderSprites) openImage(base string) (image.Image, error) {
	isJPG := false
	f, err := os.Open(base + ".png")
	if os.IsNotExist(err) {
		f, err = os.Open(base + ".jpg")
		isJPG = true
	}
	if os.IsNotExist(err) && b.zip != nil {
		if img, err := b.openImageZip(filepath.Base(base)); err == nil {
			return img, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !isJPG {
		return png.Decode(f)
	}
	return jpeg.Decode(f)
}

func parseTextureScale(data []byte) int {
	var sidecar struct {
		TextureScale json.RawMessage `json:"texture_scale"`
	}
	if err := json.Unmarshal(data, &sidecar); err != nil {
		return 1
	}
	var scale int
	if err := json.Unmarshal(sidecar.TextureScale, &scale); err != nil || !supportedTextureScale(scale) {
		return 1
	}
	return scale
}

func (b *RenderSprites) openImageMeta(base string) (*pcx.ImageMeta, int, error) {
	if jdata, err := os.ReadFile(base + ".json"); err == nil {
		var meta pcx.ImageMeta
		if err := json.Unmarshal(jdata, &meta); err != nil {
			return nil, 1, err
		}
		return &meta, parseTextureScale(jdata), nil
	} else if !os.IsNotExist(err) {
		return nil, 1, err
	}
	if b.zip == nil {
		return nil, 1, nil
	}
	zf, err := b.zip.Open(filepath.Base(base) + ".json")
	if os.IsNotExist(err) {
		return nil, 1, nil
	} else if err != nil {
		return nil, 1, err
	}
	defer zf.Close()
	jdata, err := io.ReadAll(zf)
	if err != nil {
		return nil, 1, err
	}
	var meta pcx.ImageMeta
	err = json.Unmarshal(jdata, &meta)
	if err != nil {
		return nil, 1, err
	}
	return &meta, parseTextureScale(jdata), nil
}

func (b *RenderSprites) openImageZip(base string) (image.Image, error) {
	isJPG := false
	f, err := b.zip.Open(base + ".png")
	if os.IsNotExist(err) {
		f, err = b.zip.Open(base + ".jpg")
		isJPG = true
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !isJPG {
		return png.Decode(f)
	}
	return jpeg.Decode(f)
}

func (b *RenderSprites) ImageByBagSection(sect, offs int) (*pcx.Image, error) {
	im, _, err := b.imageByBagSection(sect, offs)
	return im, err
}

// imageByBagSection is the local extension point for loose PNG sidecars. The
// upstream pcx.ImageMeta stays unchanged; texture_scale is deliberately
// consumed here so old assets and callers remain compatible.
func (b *RenderSprites) imageByBagSection(sect, offs int) (*pcx.Image, int, error) {
	b.once.Do(func() {
		if err := b.loadAndIndexVideoBag(); err != nil {
			b.err = err
			b.log.Error("cannot index bag file", "err", err)
		}
	})
	if b.err != nil {
		return nil, 1, b.err
	}
	img, ok := b.bySect[sect][offs]
	if !ok {
		return nil, 1, fmt.Errorf("image not found: %d, %d", sect, offs)
	}
	debug := debugBagImages
	if _, ok := b.seen[img]; !ok {
		b.seen[img] = struct{}{}
	} else {
		debug = false
	}
	ext := path.Ext(img.Name)
	base := strings.TrimSuffix(img.Name, ext)
	base = datapath.Data("images", base)
	im, err := b.openImage(base)
	if os.IsNotExist(err) {
		if debug {
			log.Printf("image access miss: %q", base)
		}
		return nil, 1, nil
	} else if err != nil {
		if debug {
			log.Printf("image access error: %q: %v", base, err)
		}
		return nil, 1, err
	}
	out := &pcx.Image{
		Image: im,
	}
	if mat, err := b.openImage(base + "_mat"); err == nil {
		if pal, ok := mat.(*image.Paletted); ok {
			out.Material = pal
		} else {
			log.Printf("image material error: %q: unexpected type: %T", base, mat)
		}
	}
	textureScale := 1
	if meta, scale, err := b.openImageMeta(base); err != nil {
		log.Printf("image meta error: %q: %v", base, err)
		// Invalid/missing sidecar fields intentionally fall back to scale 1.
	} else if meta != nil {
		out.ImageMeta = *meta
		textureScale = scale
	} else {
		if img.Meta == nil {
			if meta, _, err := img.DecodeHeader(); err == nil {
				img.Meta = meta
			} else {
				if debug {
					log.Printf("error decoding header: %v", err)
				}
				img.Meta = &pcx.ImageMeta{}
			}
		}
		out.ImageMeta = *img.Meta
	}
	if debug {
		log.Printf("image access: %q: type=%d, %dx%d, (%d, %d)",
			base, out.Type, out.Bounds().Dx(), out.Bounds().Dy(), out.Point.X, out.Point.Y)
	}
	return out, textureScale, nil
}
