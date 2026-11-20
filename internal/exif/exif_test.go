package exif

import (
	"math"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	thumb := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	raw := Encode(
		[]Field{ASCII(0x010F, "Google"), ASCII(0x0110, "Pixel 7"), Short(0x0112, 6), ASCII(0x013B, "Jane Roe")},
		[]Field{ASCII(0xA431, "SN-12345"), ASCII(0x9003, "2024:05:01 08:12:44"),
			{0x9286, 7, 16, append([]byte("ASCII\x00\x00\x00"), "my note!"...)}},
		[]Field{ASCII(1, "N"), Rationals(2, [2]uint32{48, 1}, [2]uint32{51, 1}, [2]uint32{2952, 100}),
			ASCII(3, "W"), Rationals(4, [2]uint32{2, 1}, [2]uint32{17, 1}, [2]uint32{4020, 100})},
		thumb,
	)
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Make != "Google" || m.Model != "Pixel 7" || m.Artist != "Jane Roe" || m.BodySerial != "SN-12345" {
		t.Fatalf("text fields: %+v", m)
	}
	if m.Orientation != 6 || m.UserComment != "my note!" || m.DateTimeOriginal != "2024:05:01 08:12:44" {
		t.Fatalf("misc fields: %+v", m)
	}
	if m.GPS == nil || math.Abs(m.GPS.Lat-48.858200) > 1e-4 || math.Abs(m.GPS.Lon+2.294500) > 1e-4 {
		t.Fatalf("gps: %+v", m.GPS)
	}
	if string(m.Thumbnail) != string(thumb) {
		t.Fatalf("thumbnail: %x", m.Thumbnail)
	}
}

func TestNullIslandIsNotALocation(t *testing.T) {
	zero := [2]uint32{0, 1}
	raw := Encode(nil, nil, []Field{ASCII(1, "N"), Rationals(2, zero, zero, zero), ASCII(3, "E"), Rationals(4, zero, zero, zero)}, nil)
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.GPS != nil {
		t.Fatalf("0,0 is what cameras write without a fix, got %v", m.GPS)
	}
}

func TestHostileInput(t *testing.T) {
	raw := Encode([]Field{ASCII(0x010F, "Canon")}, nil, nil, nil)
	for i := range raw {
		for _, v := range []byte{0, 0xFF, 0x7F} {
			b := append([]byte(nil), raw...)
			b[i] = v
			Parse(b)
		}
	}
	loop := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 0, 0, 8, 0, 0, 0}
	if _, err := Parse(loop); err != nil {
		t.Fatal(err)
	}
}
