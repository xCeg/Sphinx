package tifffile

import (
	"bytes"
	"os"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

// testdata/holiday.tiff was written by Pillow from site/examples/holiday.jpg
// with its EXIF and GPS directories.
func TestTIFF(t *testing.T) {
	b, err := os.ReadFile("testdata/holiday.tiff")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, f := range fs {
		found[f.Rule] = true
	}
	if !found[leak.GPSLocation.ID] || !found[leak.DeviceSerial.ID] {
		t.Fatalf("got %+v", fs)
	}
	sealed, err := Seal(b)
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := Inspect(sealed); len(left) != 0 {
		t.Fatalf("sealed copy still leaks: %+v", left)
	}
	if len(sealed) != len(b) || !bytes.Equal(sealed[:8], b[:8]) {
		t.Fatal("sealing blanks values in place and must keep the layout")
	}
}
