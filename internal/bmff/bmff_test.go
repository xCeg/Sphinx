package bmff

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

// holiday.heic and holiday.avif were written by pillow-heif (libheif) from
// site/examples/holiday.jpg with its EXIF block; clip.mov and clip.mp4 by
// ffmpeg with a location, a camera model and a creation time.
func rules(t *testing.T, name string, transform func([]byte) []byte) map[string]leak.Finding {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if transform != nil {
		b = transform(b)
	}
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]leak.Finding{}
	for _, f := range fs {
		out[f.Rule] = f
	}
	return out
}

func seal(b []byte) []byte {
	out, err := Seal(b)
	if err != nil {
		panic(err)
	}
	if len(out) != len(b) {
		panic("sealing must not move any byte")
	}
	return out
}

func TestHEIFAndAVIF(t *testing.T) {
	for _, name := range []string{"holiday.heic", "holiday.avif"} {
		r := rules(t, name, nil)
		if !strings.Contains(r[leak.GPSLocation.ID].Message, "45.89920° N") || r[leak.DeviceSerial.ID].Evidence != "camera body 28041JEGR10466" {
			t.Errorf("%s: %+v", name, r)
		}
		if left := rules(t, name, seal); len(left) != 0 {
			t.Errorf("%s: sealed copy still leaks: %+v", name, left)
		}
	}
}

func TestVideos(t *testing.T) {
	mov := rules(t, "clip.mov", nil)
	if !strings.Contains(mov[leak.GPSLocation.ID].Message, "filmed: 45.89920° N, 6.12940° E") {
		t.Fatalf("mov location: %+v", mov)
	}
	if !strings.Contains(mov[leak.SoftwareTrail.ID].Evidence, "model iPhone 15") {
		t.Fatalf("mov device: %+v", mov[leak.SoftwareTrail.ID])
	}
	mp4 := rules(t, "clip.mp4", nil)
	if _, ok := mp4[leak.GPSLocation.ID]; !ok {
		t.Fatalf("mp4 location (loci box): %+v", mp4)
	}
	for _, name := range []string{"clip.mov", "clip.mp4"} {
		left := rules(t, name, seal)
		for _, gone := range []*leak.Rule{leak.GPSLocation, leak.Timestamps, leak.EmbeddedText} {
			if f, ok := left[gone.ID]; ok {
				t.Errorf("%s: %s survived sealing: %+v", name, gone.Name, f)
			}
		}
	}
	orig, _ := os.ReadFile("testdata/clip.mp4")
	sealed := seal(orig)
	i := bytes.Index(orig, []byte("mdat"))
	if !bytes.Equal(orig[i:i+20000], sealed[i:i+20000]) {
		t.Fatal("media data must be untouched")
	}
}

func TestISO6709(t *testing.T) {
	for in, ok := range map[string]bool{
		"+48.8577+002.295/": true, "+48.8577+002.295+035.000/": true, "-33.8688+151.2093/": true,
		"+00.0000+000.0000/": false, "nonsense": false, "+95.0+010.0/": false,
	} {
		if got := parseISO6709(in) != nil; got != ok {
			t.Errorf("%q: got %v", in, got)
		}
	}
}

func TestHostile(t *testing.T) {
	b, _ := os.ReadFile("testdata/holiday.heic")
	for i := 0; i < 600 && i < len(b); i += 3 {
		m := append([]byte(nil), b...)
		m[i] ^= 0xFF
		Inspect(m)
		Seal(m)
		Inspect(m[:i])
	}
}
