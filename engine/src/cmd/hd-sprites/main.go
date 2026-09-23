// hd-sprites creates loose-image overrides from the player's own Nox archive.
// It contains no game art and never changes video.bag or video.idx.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/opennox/libs/bag"
	"github.com/opennox/libs/noximage/pcx"
	xdraw "golang.org/x/image/draw"
)

func spriteName(name string) (string, bool) {
	base := strings.TrimSuffix(name, path.Ext(name))
	if base == "" || base == "." || strings.ContainsAny(base, `/\:`) || strings.Contains(base, "..") {
		return "", false
	}
	for _, ch := range base {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == '~') {
			return "", false
		}
	}
	return base, true
}

func candidates(records []*bag.ImageRec) ([]*bag.ImageRec, error) {
	counts := make(map[string]int)
	for _, rec := range records {
		if rec.Type < 3 || rec.Type > 6 || rec.Size == 0 {
			continue
		}
		name, ok := spriteName(rec.Name)
		if !ok {
			return nil, fmt.Errorf("unsafe image name at index %d: %q", rec.Index, rec.Name)
		}
		counts[strings.ToLower(name)]++
	}
	var selected []*bag.ImageRec
	for _, rec := range records {
		if rec.Type < 3 || rec.Type > 6 || rec.Size == 0 {
			continue
		}
		name, _ := spriteName(rec.Name)
		if counts[strings.ToLower(name)] == 1 {
			selected = append(selected, rec)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no uniquely named drawable sprites in video.idx")
	}
	return selected, nil
}

func enlarge(src *pcx.Image, scale int) (*image.NRGBA, *image.Paletted, error) {
	if scale != 2 && scale != 4 {
		return nil, nil, errors.New("scale must be 2 or 4")
	}
	b := src.Bounds()
	if b.Empty() || b.Dx() > 1024 || b.Dy() > 1024 {
		return nil, nil, fmt.Errorf("invalid sprite bounds: %v", b)
	}
	native := image.NewNRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	xdraw.CatmullRom.Scale(native, native.Bounds(), src.Image, b, draw.Src, nil)
	if src.Material == nil {
		return native, nil, nil
	}
	if !src.Material.Bounds().Eq(b) {
		return nil, nil, errors.New("material mask bounds differ from artwork")
	}
	mask := image.NewPaletted(native.Bounds(), src.Material.Palette)
	for y := 0; y < mask.Bounds().Dy(); y++ {
		for x := 0; x < mask.Bounds().Dx(); x++ {
			mask.SetColorIndex(x, y, src.Material.ColorIndexAt(b.Min.X+x/scale, b.Min.Y+y/scale))
		}
	}
	return native, mask, nil
}

func zipPNG(z *zip.Writer, name string, img image.Image) error {
	w, err := z.Create(name)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

func zipJSON(z *zip.Writer, name string, value any) error {
	w, err := z.Create(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(value)
}

func generate(data, output string, scale int) (int, error) {
	if scale != 2 && scale != 4 {
		return 0, errors.New("scale must be 2 or 4")
	}
	data, err := filepath.Abs(data)
	if err != nil {
		return 0, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return 0, err
	}
	if strings.EqualFold(output, filepath.Join(data, "video.bag")) || strings.EqualFold(output, filepath.Join(data, "video.idx")) {
		return 0, errors.New("output cannot replace original Nox archives")
	}
	archive, err := bag.OpenWithIndex(filepath.Join(data, "video.bag"), filepath.Join(data, "video.idx"))
	if err != nil {
		return 0, err
	}
	defer archive.Close()
	records, err := archive.Images()
	if err != nil {
		return 0, err
	}
	selected, err := candidates(records)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return 0, err
	}
	stage, err := os.CreateTemp(filepath.Dir(output), ".hd-sprites-*.zip")
	if err != nil {
		return 0, err
	}
	stageName := stage.Name()
	defer os.Remove(stageName)
	z := zip.NewWriter(stage)
	masks := 0
	written := 0
	skipped := 0
	for i, rec := range selected {
		decoded, err := rec.Decode()
		if err != nil {
			skipped++
			if skipped <= 8 {
				fmt.Printf("Skipping invalid sprite %d (%s): %v\n", rec.Index, rec.Name, err)
			}
			continue
		}
		native, mask, err := enlarge(decoded, scale)
		if err != nil {
			skipped++
			if skipped <= 8 {
				fmt.Printf("Skipping invalid sprite %d (%s): %v\n", rec.Index, rec.Name, err)
			}
			continue
		}
		name, _ := spriteName(rec.Name)
		if err := zipPNG(z, name+".png", native); err != nil {
			z.Close()
			stage.Close()
			return 0, err
		}
		meta := map[string]any{"type": decoded.Type, "point": decoded.Point, "texture_scale": scale, "geometry_policy": fmt.Sprintf("local-catmullrom-%dx-v1", scale)}
		if err := zipJSON(z, name+".json", meta); err != nil {
			z.Close()
			stage.Close()
			return 0, err
		}
		if mask != nil {
			if err := zipPNG(z, name+"_mat.png", mask); err != nil {
				z.Close()
				stage.Close()
				return 0, err
			}
			masks++
		}
		written++
		if (i+1)%1000 == 0 {
			fmt.Printf("Generated %d of %d sprites\n", i+1, len(selected))
		}
	}
	if written == 0 || (len(selected) >= 1000 && written < len(selected)*95/100) {
		_ = z.Close()
		_ = stage.Close()
		return 0, fmt.Errorf("too many unreadable sprites: generated %d of %d", written, len(selected))
	}
	if err := z.Close(); err != nil {
		stage.Close()
		return 0, err
	}
	if err := stage.Close(); err != nil {
		return 0, err
	}
	// Keep the prior archive available for rollback until the complete new ZIP
	// has been closed successfully. Both paths are siblings on the same volume.
	backup := ""
	if _, err := os.Stat(output); err == nil {
		backup = output + ".previous-" + time.Now().UTC().Format("20060102-150405.000000000")
		if err := os.Rename(output, backup); err != nil {
			return 0, err
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.Rename(stageName, output); err != nil {
		if backup != "" {
			_ = os.Rename(backup, output)
		}
		return 0, err
	}
	fmt.Printf("Created %d %dx sprites and %d material masks (%d invalid records skipped): %s\n", written, scale, masks, skipped, output)
	if backup != "" {
		fmt.Printf("Previous sprite archive kept at %s\n", backup)
	}
	return written, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func main() {
	data := flag.String("data", "", "folder containing your own Nox video.bag and video.idx")
	output := flag.String("output", "", "destination video.bag.zip")
	scale := flag.Int("scale", 2, "sprite size multiplier: 2 or 4")
	flag.Parse()
	if *data == "" || *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := generate(*data, *output, *scale); err != nil {
		fmt.Fprintln(os.Stderr, "Sprite generation failed:", err)
		os.Exit(1)
	}
	if sum, err := fileHash(*output); err == nil {
		fmt.Println("SHA-256:", sum)
	}
}
