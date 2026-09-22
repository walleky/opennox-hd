// fontsprites turns an OpenType source face into OpenNox's native .fnt
// bitmap format. The glyph cells come from the installed game fonts, so the
// resulting HD sheet keeps every existing UI advance and layout stable.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/opennox/libs/noxfont"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	fontScale       = 2
	previewScale    = 4
	rasterThreshold = 96
)

var fontNames = []string{"default", "large", "small", "number"}

type manifestFont struct {
	Name       string `json:"name"`
	Glyphs     int    `json:"glyphs"`
	CellHeight int    `json:"cell_height"`
	Scale      int    `json:"scale"`
}

type manifest struct {
	Format       string         `json:"format"`
	Source       string         `json:"source"`
	SourceSHA256 string         `json:"source_sha256"`
	Fonts        []manifestFont `json:"fonts"`
}

func main() {
	var (
		templateDir string
		fontPath    string
		licensePath string
		outDir      string
	)
	flag.StringVar(&templateDir, "templates", "", "directory containing the original .fnt files")
	flag.StringVar(&fontPath, "font", "", "OpenType source font")
	flag.StringVar(&licensePath, "license", "", "optional source-font license to package beside the sprites")
	flag.StringVar(&outDir, "out", "", "directory for generated *.hd2.fnt files")
	flag.Parse()
	if templateDir == "" || fontPath == "" || outDir == "" {
		fatalf("usage: fontsprites -templates <NoxData> -font <font.ttf> -out <output>")
	}

	data, err := os.ReadFile(fontPath)
	if err != nil {
		fatalf("read source font: %v", err)
	}
	ttf, err := opentype.Parse(data)
	if err != nil {
		fatalf("parse source font: %v", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	if licensePath != "" {
		license, err := os.ReadFile(licensePath)
		if err != nil {
			fatalf("read source-font license: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outDir, "OFL.txt"), license, 0o644); err != nil {
			fatalf("write source-font license: %v", err)
		}
	}

	m := manifest{
		Format:       "opennox-hd-font-sprites-v1",
		Source:       filepath.Base(fontPath),
		SourceSHA256: sha256Hex(data),
	}
	for _, name := range fontNames {
		templatePath := filepath.Join(templateDir, name+noxfont.Ext)
		template, err := decode(templatePath)
		if err != nil {
			fatalf("decode %s: %v", templatePath, err)
		}
		out, glyphs, height, err := buildFont(ttf, template)
		if err != nil {
			fatalf("build %s: %v", name, err)
		}
		encoded, err := out.Encode()
		if err != nil {
			fatalf("encode %s: %v", name, err)
		}
		fontOut := filepath.Join(outDir, name+".hd2"+noxfont.Ext)
		if err := os.WriteFile(fontOut, encoded, 0o644); err != nil {
			fatalf("write %s: %v", fontOut, err)
		}
		if err := writePreview(filepath.Join(outDir, name+".hd2.preview.png"), out); err != nil {
			fatalf("preview %s: %v", name, err)
		}
		m.Fonts = append(m.Fonts, manifestFont{Name: name, Glyphs: glyphs, CellHeight: height, Scale: fontScale})
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), append(encoded, '\n'), 0o644); err != nil {
		fatalf("write manifest: %v", err)
	}
}

func decode(path string) (*noxfont.Font, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return noxfont.Decode(f)
}

func buildFont(ttf *opentype.Font, template *noxfont.Font) (*noxfont.Font, int, int, error) {
	if len(template.Ranges) == 0 {
		return nil, 0, 0, fmt.Errorf("font has no ranges")
	}
	cellHeight := 0
	maxWidth := 0
	glyphs := 0
	for _, rng := range template.Ranges {
		for _, glyph := range rng.Glyphs {
			if glyph.Rect.Dy() <= 0 {
				return nil, 0, 0, fmt.Errorf("invalid glyph height")
			}
			if cellHeight == 0 {
				cellHeight = glyph.Rect.Dy()
			} else if cellHeight != glyph.Rect.Dy() {
				return nil, 0, 0, fmt.Errorf("mixed glyph heights")
			}
			if glyph.Rect.Dx() > maxWidth {
				maxWidth = glyph.Rect.Dx()
			}
			glyphs++
		}
	}
	if maxWidth <= 0 || cellHeight <= 0 {
		return nil, 0, 0, fmt.Errorf("empty font geometry")
	}
	face, err := opentype.NewFace(ttf, &opentype.FaceOptions{
		Size:    float64(cellHeight * fontScale * 2),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	defer face.Close()

	outHeight := cellHeight * fontScale
	stride := (maxWidth*fontScale + 7) / 8
	out := &noxfont.Font{Field0: template.Field0, Field1: template.Field1, Inline: template.Inline}
	out.Ranges = make([]noxfont.Range, 0, len(template.Ranges))
	for _, inRange := range template.Ranges {
		rng := noxfont.Range{StartChar: inRange.StartChar, EndChar: inRange.EndChar}
		for index, inGlyph := range inRange.Glyphs {
			runeValue := rune(uint32(inRange.StartChar) + uint32(index))
			glyph := &noxfont.Bitmap{
				Pix:    make([]byte, stride*outHeight),
				Stride: stride,
				Rect:   image.Rect(0, 0, inGlyph.Rect.Dx()*fontScale, outHeight),
			}
			if err := drawGlyph(glyph, inGlyph, face, runeValue); err != nil {
				return nil, 0, 0, fmt.Errorf("glyph %q: %w", runeValue, err)
			}
			rng.Glyphs = append(rng.Glyphs, glyph)
		}
		out.Ranges = append(out.Ranges, rng)
	}
	return out, glyphs, outHeight, nil
}

func drawGlyph(dst, template *noxfont.Bitmap, face font.Face, rn rune) error {
	target := scaledBounds(opaqueBounds(template), fontScale)
	if target.Empty() {
		return nil // whitespace stays whitespace.
	}
	src, err := rasterGlyph(face, rn)
	if err != nil {
		return err
	}
	srcBounds := alphaBounds(src)
	if srcBounds.Empty() {
		return nil
	}
	scale := minFloat(float64(target.Dx())/float64(srcBounds.Dx()), float64(target.Dy())/float64(srcBounds.Dy()))
	w := maxInt(1, int(float64(srcBounds.Dx())*scale+0.5))
	h := maxInt(1, int(float64(srcBounds.Dy())*scale+0.5))
	resized := image.NewAlpha(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(resized, resized.Rect, src, srcBounds, xdraw.Src, nil)
	x0 := target.Min.X + (target.Dx()-w)/2
	y0 := target.Min.Y + (target.Dy()-h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if resized.AlphaAt(x, y).A >= rasterThreshold {
				dst.SetAlpha(x0+x, y0+y, color.Alpha{A: 255})
			}
		}
	}
	return nil
}

func rasterGlyph(face font.Face, rn rune) (*image.Alpha, error) {
	bounds, _, ok := face.GlyphBounds(rn)
	if !ok {
		return image.NewAlpha(image.Rectangle{}), nil
	}
	minX, minY := bounds.Min.X.Floor(), bounds.Min.Y.Floor()
	maxX, maxY := bounds.Max.X.Ceil(), bounds.Max.Y.Ceil()
	if maxX <= minX || maxY <= minY {
		return image.NewAlpha(image.Rectangle{}), nil
	}
	const pad = 2
	dst := image.NewAlpha(image.Rect(0, 0, maxX-minX+2*pad, maxY-minY+2*pad))
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(pad-minX, pad-minY),
	}
	d.DrawString(string(rn))
	return dst, nil
}

func opaqueBounds(g *noxfont.Bitmap) image.Rectangle {
	if g == nil {
		return image.Rectangle{}
	}
	b := g.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.AlphaModel.Convert(g.At(x, y)).(color.Alpha).A == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func alphaBounds(img *image.Alpha) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	b := img.Rect
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.AlphaAt(x, y).A == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func scaledBounds(r image.Rectangle, scale int) image.Rectangle {
	return image.Rect(r.Min.X*scale, r.Min.Y*scale, r.Max.X*scale, r.Max.Y*scale)
}

func writePreview(path string, f *noxfont.Font) error {
	maxW, height, glyphs := 0, 0, 0
	for _, rng := range f.Ranges {
		for _, glyph := range rng.Glyphs {
			maxW = maxInt(maxW, glyph.Rect.Dx())
			height = maxInt(height, glyph.Rect.Dy())
			glyphs++
		}
	}
	if maxW == 0 || height == 0 || glyphs == 0 {
		return fmt.Errorf("empty font")
	}
	const columns = 16
	rows := (glyphs + columns - 1) / columns
	cell := image.Pt((maxW+2)*previewScale, (height+2)*previewScale)
	preview := image.NewNRGBA(image.Rect(0, 0, columns*cell.X, rows*cell.Y))
	for y := range preview.Pix {
		preview.Pix[y] = 18
	}
	i := 0
	for _, rng := range f.Ranges {
		for _, glyph := range rng.Glyphs {
			x0 := (i%columns)*cell.X + previewScale
			y0 := (i/columns)*cell.Y + previewScale
			for y := glyph.Rect.Min.Y; y < glyph.Rect.Max.Y; y++ {
				for x := glyph.Rect.Min.X; x < glyph.Rect.Max.X; x++ {
					if color.AlphaModel.Convert(glyph.At(x, y)).(color.Alpha).A == 0 {
						continue
					}
					for sy := 0; sy < previewScale; sy++ {
						for sx := 0; sx < previewScale; sx++ {
							p := preview.PixOffset(x0+(x-glyph.Rect.Min.X)*previewScale+sx, y0+(y-glyph.Rect.Min.Y)*previewScale+sy)
							preview.Pix[p+0], preview.Pix[p+1], preview.Pix[p+2], preview.Pix[p+3] = 240, 232, 202, 255
						}
					}
				}
			}
			i++
		}
	}
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	return png.Encode(fh, preview)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func minFloat(a, b float64) float64 {
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
