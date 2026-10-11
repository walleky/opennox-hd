package noxrender

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedSpriteArchiveRequiresMatchingScale(t *testing.T) {
	for _, scale := range []int{2, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "sprites.zip")
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			w, err := z.Create("sprite.json")
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(w, `{"texture_scale":%d}`, scale)
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NOX_SPRITE_ARCHIVE", name)
			t.Setenv("NOX_TEXTURE_DENSITY_SCALE", fmt.Sprint(scale))
			var b RenderSprites
			if err := b.openVideoZip(); err != nil {
				t.Fatal(err)
			}
			b.zip.Close()
			t.Setenv("NOX_TEXTURE_DENSITY_SCALE", fmt.Sprint(6-scale))
			if err := b.openVideoZip(); err == nil {
				t.Fatal("mismatched archive accepted")
			}
		})
	}
}

func TestSelectedSpriteArchiveMissingFailsAndOriginalSkipsIt(t *testing.T) {
	t.Setenv("NOX_SPRITE_ARCHIVE", filepath.Join(t.TempDir(), "missing.zip"))
	var b RenderSprites
	if err := b.openVideoZip(); err == nil {
		t.Fatal("missing selected archive accepted")
	}
	t.Setenv("NOX_SPRITE_ARCHIVE", "none")
	if err := b.openVideoZip(); err != nil || b.zip != nil {
		t.Fatalf("original mode loaded a ZIP: %v", err)
	}
}
