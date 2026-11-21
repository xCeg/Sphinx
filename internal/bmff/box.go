// Package bmff inspects and seals files built on the ISO base media file
// format: HEIF and AVIF photos, and MP4 and QuickTime videos. Both keep
// their metadata in boxes; videos record where they were filmed in a
// handful of box types that no player shows.
package bmff

import (
	"encoding/binary"
	"errors"
)

type box struct {
	typ    string
	start  int // offset of the box header
	body   int // offset of the payload
	end    int
	parent *box
}

func (b box) data(f []byte) []byte { return f[b.body:b.end] }

var errNotBMFF = errors.New("not an ISO media file")

// containers lists boxes whose payload is a sequence of boxes, and how many
// bytes of version and flags precede it.
var containers = map[string]int{
	"moov": 0, "trak": 0, "mdia": 0, "minf": 0, "stbl": 0, "udta": 0, "edts": 0,
	"meta": 4, "ilst": 0, "iprp": 0, "ipco": 0, "dinf": 0, "moof": 0, "traf": 0,
}

// walk parses boxes between start and end, calling visit for each, and
// descends into containers. It stops at the first malformed size.
func walk(f []byte, start, end int, parent *box, depth int, visit func(*box)) {
	for p := start; p+8 <= end && depth < 12; {
		n := int(binary.BigEndian.Uint32(f[p:]))
		typ := string(f[p+4 : p+8])
		h := 8
		switch n {
		case 1:
			if p+16 > end {
				return
			}
			n64 := binary.BigEndian.Uint64(f[p+8:])
			if n64 > uint64(end-p) {
				return
			}
			n, h = int(n64), 16
		case 0:
			n = end - p
		}
		if n < h || p+n > end {
			return
		}
		b := &box{typ: typ, start: p, body: p + h, end: p + n, parent: parent}
		if typ == "uuid" && b.body+16 <= b.end {
			b.body += 16
		}
		visit(b)
		if skip, ok := containers[typ]; ok && b.body+skip <= b.end {
			// QuickTime writes meta without the version field; a valid
			// child header right away tells the two layouts apart.
			if typ == "meta" && b.body+8 <= b.end && isBoxType(f[b.body+4:b.body+8]) {
				skip = 0
			}
			walk(f, b.body+skip, b.end, b, depth+1, visit)
		}
		p += n
	}
}

func isBoxType(t []byte) bool {
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == 0xA9) {
			return false
		}
	}
	return true
}

func (b *box) path() string {
	if b.parent == nil {
		return b.typ
	}
	return b.parent.path() + "/" + b.typ
}

// Brand returns the major brand of an ftyp box at the start of f.
func Brand(f []byte) string {
	if len(f) < 12 || string(f[4:8]) != "ftyp" {
		return ""
	}
	return string(f[8:12])
}

var heifBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true, "hevx": true,
	"mif1": true, "msf1": true, "avif": true, "avis": true,
}

// IsHEIF tells still-image containers from videos by their brands.
func IsHEIF(f []byte) bool {
	if heifBrands[Brand(f)] {
		return true
	}
	if len(f) < 16 {
		return false
	}
	n := int(binary.BigEndian.Uint32(f))
	for p := 16; p+4 <= n && p+4 <= len(f); p += 4 {
		if heifBrands[string(f[p:p+4])] && string(f[p:p+4]) != "mif1" {
			return true
		}
	}
	return false
}
