// Package jpegfile inspects, reveals and seals JPEG images.
package jpegfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

type segment struct {
	Marker byte
	Offset int
	Data   []byte
	// Scan is the entropy-coded data that follows an SOS header.
	Scan []byte
}

type file struct {
	segments []segment
	end      int
	trailing []byte
}

var errNotJPEG = errors.New("not a JPEG file")

func parse(b []byte) (*file, error) {
	if !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		return nil, errNotJPEG
	}
	f := &file{}
	p := 2
	for {
		for p < len(b) && b[p] != 0xFF {
			p++
		}
		for p+1 < len(b) && b[p+1] == 0xFF {
			p++
		}
		if p+1 >= len(b) {
			return nil, fmt.Errorf("JPEG ends early at byte %d, before its end-of-image marker", p)
		}
		m := b[p+1]
		start := p
		p += 2
		if m == 0xD9 {
			f.end = p
			f.trailing = b[p:]
			return f, nil
		}
		if m == 0x01 || (m >= 0xD0 && m <= 0xD7) {
			continue
		}
		if p+2 > len(b) {
			return nil, fmt.Errorf("JPEG segment at byte %d is cut off", start)
		}
		n := int(binary.BigEndian.Uint16(b[p:]))
		if n < 2 || p+n > len(b) {
			return nil, fmt.Errorf("JPEG segment at byte %d claims %d bytes, more than the file holds", start, n)
		}
		seg := segment{Marker: m, Offset: start, Data: b[p+2 : p+n]}
		p += n
		if m == 0xDA {
			q := scanEnd(b, p)
			seg.Scan = b[p:q]
			p = q
		}
		f.segments = append(f.segments, seg)
	}
}

// scanEnd finds the marker that ends entropy-coded data: 0xFF followed by
// anything but a stuffed zero or a restart marker.
func scanEnd(b []byte, p int) int {
	for {
		i := bytes.IndexByte(b[p:], 0xFF)
		if i < 0 {
			return len(b)
		}
		p += i
		if p+1 >= len(b) {
			return len(b)
		}
		next := b[p+1]
		if next == 0 || (next >= 0xD0 && next <= 0xD7) || next == 0xFF {
			p++
			continue
		}
		return p
	}
}

func (s segment) app(prefix string) ([]byte, bool) {
	if s.Marker < 0xE0 || s.Marker > 0xEF || !bytes.HasPrefix(s.Data, []byte(prefix)) {
		return nil, false
	}
	return s.Data[len(prefix):], true
}

func appendSegment(out []byte, marker byte, data []byte) []byte {
	out = append(out, 0xFF, marker)
	out = binary.BigEndian.AppendUint16(out, uint16(len(data)+2))
	return append(out, data...)
}
