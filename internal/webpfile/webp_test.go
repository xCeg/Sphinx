package webpfile

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

// testdata/holiday.webp is site/examples/holiday.jpg converted by
// ImageMagick 6 with libwebp 1.3.2, which carries the EXIF block over.
func TestImageMagickWebP(t *testing.T) {
	b, err := os.ReadFile("testdata/holiday.webp")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, f := range fs {
		rules[f.Rule] = true
	}
	if !rules[leak.GPSLocation.ID] || !rules[leak.DeviceSerial.ID] {
		t.Fatalf("got %+v", fs)
	}
	sealed, err := Seal(b)
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := Inspect(sealed); len(left) != 0 {
		t.Fatalf("sealed file still leaks: %+v", left)
	}
	chunks, _, err := parse(sealed)
	if err != nil || chunks[0].id != "VP8X" || chunks[0].data[0]&0x08 != 0 {
		t.Fatalf("VP8X must stay first with its EXIF flag cleared: %v %+v", err, chunks)
	}
	if size := binary.LittleEndian.Uint32(sealed[4:]); int(size) != len(sealed)-8 {
		t.Fatalf("RIFF size %d for %d bytes", size, len(sealed))
	}
}

func TestTrailingDataAndC2PA(t *testing.T) {
	b, _ := os.ReadFile("testdata/holiday.webp")
	chunks, _, _ := parse(b)
	body := []byte("WEBP")
	for _, c := range chunks {
		body = append(body, c.id...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(c.data)))
		body = append(body, c.data...)
		if len(c.data)%2 == 1 {
			body = append(body, 0)
		}
	}
	manifest := append([]byte("....jumb....jumdc2pa...claim_generator"), 0x6C)
	manifest = append(manifest, "Photoshop 26"...)
	body = append(body, "C2PA"...)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(manifest)))
	body = append(body, manifest...)
	if len(manifest)%2 == 1 {
		body = append(body, 0)
	}
	file := append(append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...), body...)
	file = append(file, "appended secret"...)
	fs, _ := Inspect(file)
	var c2pa, trailing bool
	for _, f := range fs {
		c2pa = c2pa || f.Rule == leak.ContentCredentials.ID && f.Evidence == "Photoshop 26"
		trailing = trailing || f.Rule == leak.TrailingData.ID
	}
	if !c2pa || !trailing {
		t.Fatalf("got %+v", fs)
	}
}
