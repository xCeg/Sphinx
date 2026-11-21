package pngfile

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"testing"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
)

// screen draws something with the statistics of a screenshot: flat areas,
// sharp edges and short runs of high-contrast "glyphs".
func screen(w, h int, opaque bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(int64(w * h)))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8(r.Intn(6))
			c := color.NRGBA{240 + n, 240 + n, uint8(200 + x/8), 255}
			if (y/24)%5 == 0 {
				c = color.NRGBA{30 + n, 60, uint8(100 + y/8), 255}
			}
			if y%24 > 6 && y%24 < 18 && (x/7+y/24)%3 != 0 && r.Intn(4) != 0 {
				g := uint8(r.Intn(90))
				c = color.NRGBA{g, g, g, 255}
			}
			if !opaque {
				c.A = 200
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// acropalypse reproduces the bug: the cropped image is written over the
// original file, which is not truncated.
func acropalypse(t *testing.T, full []byte, crop image.Rectangle) ([]byte, []byte, *image.NRGBA) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	orig := image.NewNRGBA(img.Bounds())
	draw.Draw(orig, orig.Bounds(), img, image.Point{}, draw.Src)
	small := encode(t, orig.SubImage(crop))
	if len(small) >= len(full) {
		t.Fatal("crop must be smaller than the original")
	}
	return append(small, full[len(small):]...), small, orig
}

func screenshot(t *testing.T) []byte {
	b, err := os.ReadFile("testdata/screenshot.png")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// edges marks pixels with a strong horizontal luminance step. Recovered
// pixels can drift in colour, but text and borders keep their edges.
func edges(img image.Image) map[image.Point]bool {
	out := map[image.Point]bool{}
	b := img.Bounds()
	lum := func(x, y int) int {
		r, g, bl, _ := img.At(x, y).RGBA()
		return int(r>>8+g>>8+bl>>8) / 3
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X + 1; x < b.Max.X; x++ {
			d := lum(x, y) - lum(x-1, y)
			if d > 60 || d < -60 {
				out[image.Pt(x-b.Min.X, y-b.Min.Y)] = true
			}
		}
	}
	return out
}

func TestHiddenCropIsFoundAndRecovered(t *testing.T) {
	file, _, orig := acropalypse(t, screenshot(t), image.Rect(0, 0, 540, 300))

	got, err := Inspect(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Rule != leak.HiddenCrop.ID {
		t.Fatalf("want %s, got %+v", leak.HiddenCrop.ID, got)
	}

	arts, err := Reveal(file)
	if err != nil || len(arts) != 1 {
		t.Fatalf("reveal: %v %v", arts, err)
	}
	rec, err := png.Decode(bytes.NewReader(arts[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	rb := rec.Bounds()
	if rb.Dx() != 540 || rb.Dy() < 300 {
		t.Fatalf("recovered %v, want 540 px wide and at least 300 rows", rb)
	}
	want := edges(orig.SubImage(image.Rect(0, 1000-rb.Dy(), 540, 1000)))
	have := edges(rec)
	match := 0
	for p := range want {
		if have[p] {
			match++
		}
	}
	if match*100 < len(want)*80 {
		t.Fatalf("only %d of %d edges of the original survive in the recovery", match, len(want))
	}
}

func TestSyntheticCropOfAnotherEncoder(t *testing.T) {
	file, _, _ := acropalypse(t, encode(t, screen(360, 800, true)), image.Rect(0, 0, 360, 160))
	got, _ := Inspect(file)
	if len(got) == 0 || got[0].Rule != leak.HiddenCrop.ID {
		t.Fatalf("want %s, got %+v", leak.HiddenCrop.ID, got)
	}
}

func TestCleanPNGHasNoFindings(t *testing.T) {
	got, err := Inspect(encode(t, screen(64, 64, true)))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTrailingJunkIsNotMistakenForACrop(t *testing.T) {
	file := append(encode(t, screen(64, 64, true)), []byte("hello, this was appended by a tool")...)
	got, _ := Inspect(file)
	if len(got) != 1 || got[0].Rule != leak.TrailingData.ID {
		t.Fatalf("got %+v", got)
	}
}

func withChunks(t *testing.T, extra ...[2]string) []byte {
	b := encode(t, screen(32, 32, true))
	f, err := parse(b)
	if err != nil {
		t.Fatal(err)
	}
	out := append([]byte(nil), signature...)
	for _, c := range f.chunks {
		if c.Type == "IEND" {
			for _, e := range extra {
				out = appendChunk(out, e[0], []byte(e[1]))
			}
		}
		out = appendChunk(out, c.Type, c.Data)
	}
	return out
}

func TestTextChunks(t *testing.T) {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte("Jane Roe"))
	w.Close()
	gps := exif.Encode(nil, nil, []exif.Field{
		exif.ASCII(1, "N"), exif.Rationals(2, [2]uint32{40, 1}, [2]uint32{44, 1}, [2]uint32{0, 1}),
		exif.ASCII(3, "W"), exif.Rationals(4, [2]uint32{73, 1}, [2]uint32{59, 1}, [2]uint32{0, 1}),
	}, nil)
	file := withChunks(t,
		[2]string{"tEXt", "parameters\x00a castle at dusk, Steps: 30, Seed: 42"},
		[2]string{"zTXt", "Author\x00\x00" + z.String()},
		[2]string{"eXIf", string(gps)},
	)
	got, err := Inspect(file)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]leak.Severity{}
	for _, f := range got {
		rules[f.Rule] = f.Severity
	}
	if rules[leak.EmbeddedText.ID] != leak.Medium || rules[leak.PersonalNames.ID] == 0 || rules[leak.GPSLocation.ID] != leak.High {
		t.Fatalf("got %+v", got)
	}

	sealed, err := Seal(file)
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := Inspect(sealed); len(left) != 0 {
		t.Fatalf("sealed file still leaks: %+v", left)
	}
	if _, err := png.Decode(bytes.NewReader(sealed)); err != nil {
		t.Fatalf("sealed file is not a valid PNG: %v", err)
	}
}

func TestSealDropsHiddenCrop(t *testing.T) {
	file, small, _ := acropalypse(t, screenshot(t), image.Rect(0, 0, 540, 200))
	sealed, err := Seal(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sealed, small) {
		t.Fatal("sealing should give back exactly the visible PNG")
	}
}

func TestTruncatedPNG(t *testing.T) {
	b := encode(t, screen(32, 32, true))
	if _, err := Inspect(b[:len(b)-20]); err == nil {
		t.Fatal("expected an error for a truncated file")
	}
}

func TestRepeatedIENDIsNotALeak(t *testing.T) {
	b := append(encode(t, screen(16, 16, true)), "\x00\x00\x00\x00IEND\xaeB`\x82"...)
	if got, _ := Inspect(b); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
