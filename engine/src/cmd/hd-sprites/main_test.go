package main

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func fixtureArchive(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	sprite := make([]byte, 17)
	binary.LittleEndian.PutUint32(sprite[0:], 2)
	binary.LittleEndian.PutUint32(sprite[4:], 1)
	binary.LittleEndian.PutUint32(sprite[8:], 3)
	binary.LittleEndian.PutUint32(sprite[12:], 4)
	// One opaque RGB555 pixel followed by one material-colored pixel.
	sprite = append(sprite, 2, 1, 0x00, 0x7c, 4, 1, 0x80)
	validLength := len(sprite)
	sprite = append(sprite, make([]byte, 17)...) // Invalid 0x0 placeholder, as in Nox's index.
	if err := os.WriteFile(filepath.Join(data, "video.bag"), sprite, 0600); err != nil {
		t.Fatal(err)
	}
	idx := make([]byte, 24)
	binary.LittleEndian.PutUint32(idx[0:], 0xFAEDBCEA)
	binary.LittleEndian.PutUint32(idx[8:], 1)
	segment := make([]byte, 12)
	binary.LittleEndian.PutUint32(segment[4:], uint32(len(sprite)))
	binary.LittleEndian.PutUint32(segment[8:], 2)
	idx = append(idx, segment...)
	idx = append(idx, 9)
	idx = append(idx, []byte("test.pcx\x00")...)
	idx = append(idx, 3)
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, uint32(validLength))
	idx = append(idx, length...)
	idx = append(idx, length...)
	idx = append(idx, 9)
	idx = append(idx, []byte("null.pcx\x00")...)
	idx = append(idx, 3)
	binary.LittleEndian.PutUint32(length, 17)
	idx = append(idx, length...)
	idx = append(idx, length...)
	if err := os.WriteFile(filepath.Join(data, "video.idx"), idx, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGenerateOwnedArchive(t *testing.T) {
	data := fixtureArchive(t)
	for _, scale := range []int{2, 4} {
		t.Run(string(rune('0'+scale)), func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "video.bag.zip")
			count, err := generate(data, output, scale)
			if err != nil || count != 1 {
				t.Fatalf("generate: count=%d err=%v", count, err)
			}
			zf, err := zip.OpenReader(output)
			if err != nil {
				t.Fatal(err)
			}
			defer zf.Close()
			if len(zf.File) != 3 {
				t.Fatalf("ZIP entries: %d", len(zf.File))
			}
			contents := make(map[string]*zip.File)
			for _, f := range zf.File {
				contents[f.Name] = f
			}
			decode := func(name string) image.Image {
				stream, err := contents[name].Open()
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				img, err := png.Decode(stream)
				if err != nil {
					t.Fatal(err)
				}
				return img
			}
			art := decode("test.png")
			if art.Bounds().Dx() != 2*scale || art.Bounds().Dy() != scale {
				t.Fatalf("art bounds: %v", art.Bounds())
			}
			mask, ok := decode("test_mat.png").(*image.Paletted)
			if !ok || mask.Bounds() != art.Bounds() || mask.ColorIndexAt(0, 0) != 0 || mask.ColorIndexAt(scale, 0) != 1 {
				t.Fatalf("material mask invalid: %T", mask)
			}
			stream, err := contents["test.json"].Open()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var meta struct {
				Type         int         `json:"type"`
				Point        image.Point `json:"point"`
				TextureScale int         `json:"texture_scale"`
			}
			if err := json.NewDecoder(stream).Decode(&meta); err != nil {
				t.Fatal(err)
			}
			if meta.Type != 3 || meta.Point != (image.Pt(3, 4)) || meta.TextureScale != scale {
				t.Fatalf("metadata: %+v", meta)
			}
		})
	}
}

func TestOriginalArchiveNeverReplaced(t *testing.T) {
	data := fixtureArchive(t)
	if _, err := generate(data, filepath.Join(data, "video.bag"), 2); err == nil {
		t.Fatal("source archive replacement was allowed")
	}
	if _, err := generate(data, filepath.Join(data, "video.idx"), 4); err == nil {
		t.Fatal("source index replacement was allowed")
	}
}
