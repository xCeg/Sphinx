package jpegfile

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
)

func scene(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 120, 255})
		}
	}
	draw.Draw(img, image.Rect(w/10, h/5, w/3, h*4/5), image.NewUniform(color.RGBA{250, 250, 250, 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(w*2/3, h/3, w*9/10, h*2/3), image.NewUniform(color.RGBA{10, 10, 10, 255}), image.Point{}, draw.Src)
	return img
}

func resize(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	for y := range h {
		for x := range w {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	return dst
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withApps inserts segments right after SOI, as cameras do.
func withApps(jpg []byte, segs ...[]byte) []byte {
	out := []byte{0xFF, 0xD8}
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, jpg[2:]...)
}

func app(marker byte, data []byte) []byte { return appendSegment(nil, marker, data) }

func exifApp(ifd0, sub, gps []exif.Field, thumb []byte) []byte {
	return app(0xE1, append([]byte(exifPrefix), exif.Encode(ifd0, sub, gps, thumb)...))
}

var paris = []exif.Field{
	exif.ASCII(1, "N"), exif.Rationals(2, [2]uint32{48, 1}, [2]uint32{51, 1}, [2]uint32{2952, 100}),
	exif.ASCII(3, "E"), exif.Rationals(4, [2]uint32{2, 1}, [2]uint32{17, 1}, [2]uint32{4020, 100}),
}

func rules(fs []leak.Finding) map[string]leak.Finding {
	m := map[string]leak.Finding{}
	for _, f := range fs {
		m[f.Rule] = f
	}
	return m
}

func TestPhoneLikePhoto(t *testing.T) {
	img := scene(600, 400)
	thumb := encode(t, resize(img, 160, 107))
	file := withApps(encode(t, img), exifApp(
		[]exif.Field{exif.ASCII(0x010F, "Google"), exif.ASCII(0x0110, "Pixel 7"), exif.Short(0x0112, 6)},
		[]exif.Field{exif.ASCII(0x9003, "2024:05:01 08:12:44"), exif.ASCII(0xA431, "28041JEGR10466")},
		paris, thumb))

	got, err := Inspect(file)
	if err != nil {
		t.Fatal(err)
	}
	r := rules(got)
	for _, want := range []*leak.Rule{leak.GPSLocation, leak.DeviceSerial, leak.Timestamps, leak.SoftwareTrail} {
		if _, ok := r[want.ID]; !ok {
			t.Errorf("missing %s in %+v", want.Name, got)
		}
	}
	if _, ok := r[leak.ThumbnailMismatch.ID]; ok {
		t.Error("an up-to-date preview was reported as different")
	}
	if !strings.Contains(r[leak.GPSLocation.ID].Message, "48.85820° N, 2.29450° E") {
		t.Errorf("gps message: %s", r[leak.GPSLocation.ID].Message)
	}

	sealed, err := Seal(file)
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := Inspect(sealed); len(left) != 0 {
		t.Fatalf("sealed photo still leaks: %+v", left)
	}
	f, _ := parse(sealed)
	if m := read(f).exif; m == nil || m.Orientation != 6 {
		t.Fatal("sealing must keep the orientation, or portraits turn sideways")
	}
}

func TestCroppedPhotoWithStalePreview(t *testing.T) {
	img := scene(600, 400)
	stale := encode(t, resize(img, 160, 107))
	croppedImg := img.SubImage(image.Rect(300, 100, 600, 400))
	file := withApps(encode(t, croppedImg), exifApp(nil, nil, nil, stale))

	got, _ := Inspect(file)
	f, ok := rules(got)[leak.ThumbnailMismatch.ID]
	if !ok || !strings.Contains(f.Message, "cropped") {
		t.Fatalf("got %+v", got)
	}
	arts, _ := Reveal(file)
	if len(arts) != 1 || !bytes.Equal(arts[0].Data, stale) {
		t.Fatalf("reveal should hand back the stale preview, got %d artifacts", len(arts))
	}
}

func TestLetterboxedPreviewIsNotAMismatch(t *testing.T) {
	img := scene(600, 400)
	box := image.NewRGBA(image.Rect(0, 0, 160, 120))
	draw.Draw(box, box.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(box, image.Rect(0, 7, 160, 113), resize(img, 160, 106), image.Point{}, draw.Src)
	file := withApps(encode(t, img), exifApp(nil, nil, nil, encode(t, box)))
	if got, _ := Inspect(file); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func mp4(seconds uint32, audio bool) []byte {
	box := func(typ string, body ...[]byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, 0)
		b = append(b, typ...)
		for _, x := range body {
			b = append(b, x...)
		}
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		return b
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], seconds*1000)
	hdlr := func(kind string) []byte {
		h := make([]byte, 24)
		copy(h[8:], kind)
		return box("trak", box("mdia", box("hdlr", h)))
	}
	moov := box("moov", box("mvhd", mvhd), hdlr("vide"))
	if audio {
		moov = box("moov", box("mvhd", mvhd), hdlr("vide"), hdlr("soun"))
	}
	out := append(box("ftyp", []byte("mp42\x00\x00\x00\x00isom")), moov...)
	return append(out, box("mdat", make([]byte, 4096))...)
}

func TestMotionPhoto(t *testing.T) {
	file := append(encode(t, scene(300, 200)), mp4(3, true)...)
	got, _ := Inspect(file)
	f, ok := rules(got)[leak.HiddenVideo.ID]
	if !ok || !strings.Contains(f.Message, "3.0-second video") || !strings.Contains(f.Message, "with sound") {
		t.Fatalf("got %+v", got)
	}
	sealed, err := Seal(file)
	if err != nil || bytes.Contains(sealed, []byte("ftyp")) {
		t.Fatalf("sealed file still carries the video (%v)", err)
	}
}

func TestGainMapIsNotALeak(t *testing.T) {
	img := scene(600, 400)
	gain := image.NewGray(image.Rect(0, 0, 150, 100))
	draw.Draw(gain, gain.Bounds(), resize(img, 150, 100), image.Point{}, draw.Src)
	file := append(encode(t, img), encode(t, gain)...)
	if got, _ := Inspect(file); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestEncoderCommentIsOnlyASoftwareTrail(t *testing.T) {
	file := withApps(encode(t, scene(64, 64)), app(0xFE, []byte("CREATOR: gd-jpeg v1.0 (using IJG JPEG v62), quality = 82\n")))
	got, _ := Inspect(file)
	if len(got) != 1 || got[0].Rule != leak.SoftwareTrail.ID || got[0].Severity != leak.Low {
		t.Fatalf("got %+v", got)
	}
}

func TestIPTCByline(t *testing.T) {
	rec := []byte{0x1C, 2, iptcByline, 0, 8}
	rec = append(rec, "Jane Roe"...)
	rec = append(rec, 0x1C, 2, iptcCity, 0, 5)
	rec = append(rec, "Lyon "...)
	block := append([]byte("8BIM\x04\x04\x00\x00"), binary.BigEndian.AppendUint32(nil, uint32(len(rec)))...)
	block = append(block, rec...)
	file := withApps(encode(t, scene(64, 64)), app(0xED, append([]byte(psPrefix), block...)))
	r := rules(must(Inspect(file)))
	if r[leak.PersonalNames.ID].Evidence != "Jane Roe" || r[leak.GPSLocation.ID].Evidence != "Lyon" {
		t.Fatalf("got %+v", r)
	}
}

func TestTrailingJunk(t *testing.T) {
	file := append(encode(t, scene(64, 64)), []byte("user=jroe;session=abc123")...)
	got := must(Inspect(file))
	if len(got) != 1 || got[0].Rule != leak.TrailingData.ID {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(got[0].Evidence, "user=jroe") {
		t.Fatalf("evidence should preview the text: %q", got[0].Evidence)
	}
}

func TestPaddingIsIgnored(t *testing.T) {
	file := append(encode(t, scene(64, 64)), make([]byte, 512)...)
	if got := must(Inspect(file)); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestTruncated(t *testing.T) {
	b := encode(t, scene(64, 64))
	if _, err := Inspect(b[:len(b)/2]); err == nil {
		t.Fatal("expected an error")
	}
}

func must(f []leak.Finding, err error) []leak.Finding {
	if err != nil {
		panic(err)
	}
	return f
}
