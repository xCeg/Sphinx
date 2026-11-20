// Package exif reads the parts of an EXIF/TIFF block that identify a person,
// a place or a device. It never trusts offsets: every read is bounds-checked
// and IFD chains are walked at most once.
package exif

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

type GPS struct {
	Lat, Lon float64
	Alt      *float64
}

func (g GPS) String() string {
	ns, ew := "N", "E"
	if g.Lat < 0 {
		ns = "S"
	}
	if g.Lon < 0 {
		ew = "W"
	}
	s := fmt.Sprintf("%.5f° %s, %.5f° %s", math.Abs(g.Lat), ns, math.Abs(g.Lon), ew)
	if g.Alt != nil {
		s += fmt.Sprintf(", %.0f m", *g.Alt)
	}
	return s
}

func (g GPS) MapURL() string {
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.5f&mlon=%.5f#map=17/%.5f/%.5f", g.Lat, g.Lon, g.Lat, g.Lon)
}

// Meta holds the identifying fields. Text fields are trimmed; empty means absent.
type Meta struct {
	Make, Model, Software, HostComputer, LensModel string
	Artist, Owner, Copyright, XPAuthor             string
	BodySerial, LensSerial, UniqueID               string
	Description, UserComment, XPComment, XPSubject string
	DateTime, DateTimeOriginal, DateTimeDigitized  string
	OffsetTime                                     string
	GPS                                            *GPS
	Orientation                                    int
	Thumbnail                                      []byte
}

type tag uint16

const (
	tagDescription  tag = 0x010E
	tagMake         tag = 0x010F
	tagModel        tag = 0x0110
	tagOrientation  tag = 0x0112
	tagSoftware     tag = 0x0131
	tagDateTime     tag = 0x0132
	tagArtist       tag = 0x013B
	tagHostComputer tag = 0x013C
	tagThumbOffset  tag = 0x0201
	tagThumbLength  tag = 0x0202
	tagCopyright    tag = 0x8298
	tagExifIFD      tag = 0x8769
	tagGPSIFD       tag = 0x8825
	tagDTOriginal   tag = 0x9003
	tagDTDigitized  tag = 0x9004
	tagOffsetTime   tag = 0x9010
	tagUserComment  tag = 0x9286
	tagXPComment    tag = 0x9C9C
	tagXPAuthor     tag = 0x9C9D
	tagXPSubject    tag = 0x9C9F
	tagUniqueID     tag = 0xA420
	tagOwner        tag = 0xA430
	tagBodySerial   tag = 0xA431
	tagLensModel    tag = 0xA434
	tagLensSerial   tag = 0xA435
	gpsLatRef       tag = 1
	gpsLat          tag = 2
	gpsLonRef       tag = 3
	gpsLon          tag = 4
	gpsAltRef       tag = 5
	gpsAlt          tag = 6
)

var ErrNotTIFF = errors.New("exif: not a TIFF structure")

type reader struct {
	b       []byte
	order   binary.ByteOrder
	visited map[uint32]bool
}

type entry struct {
	typ   uint16
	count uint32
	value []byte
}

var typeSize = map[uint16]uint32{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

// Parse reads a TIFF structure, as found after the "Exif\0\0" prefix of a
// JPEG APP1 segment or in a PNG eXIf chunk.
func Parse(b []byte) (*Meta, error) {
	if len(b) < 8 {
		return nil, ErrNotTIFF
	}
	r := &reader{b: b, visited: map[uint32]bool{}}
	switch string(b[:2]) {
	case "II":
		r.order = binary.LittleEndian
	case "MM":
		r.order = binary.BigEndian
	default:
		return nil, ErrNotTIFF
	}
	if r.order.Uint16(b[2:]) != 42 {
		return nil, ErrNotTIFF
	}
	m := &Meta{}
	ifd0, next := r.ifd(r.order.Uint32(b[4:]))
	r.fill(m, ifd0)
	if off, ok := r.long(ifd0[tagExifIFD]); ok {
		sub, _ := r.ifd(off)
		r.fill(m, sub)
	}
	if off, ok := r.long(ifd0[tagGPSIFD]); ok {
		gps, _ := r.ifd(off)
		m.GPS = r.gps(gps)
	}
	if next != 0 {
		ifd1, _ := r.ifd(next)
		off, ok1 := r.long(ifd1[tagThumbOffset])
		n, ok2 := r.long(ifd1[tagThumbLength])
		if ok1 && ok2 && uint64(off)+uint64(n) <= uint64(len(b)) && n > 0 {
			m.Thumbnail = b[off : off+n]
		}
	}
	return m, nil
}

func (r *reader) ifd(off uint32) (map[tag]entry, uint32) {
	out := map[tag]entry{}
	if r.visited[off] || uint64(off)+2 > uint64(len(r.b)) {
		return out, 0
	}
	r.visited[off] = true
	n := uint32(r.order.Uint16(r.b[off:]))
	base := off + 2
	if uint64(base)+uint64(n)*12+4 > uint64(len(r.b)) {
		return out, 0
	}
	for i := uint32(0); i < n; i++ {
		e := r.b[base+i*12:]
		typ, count := r.order.Uint16(e[2:]), r.order.Uint32(e[4:])
		size, ok := typeSize[typ]
		if !ok || count > 1<<24 {
			continue
		}
		total := size * count
		var val []byte
		if total <= 4 {
			val = e[8 : 8+total]
		} else {
			p := r.order.Uint32(e[8:])
			if uint64(p)+uint64(total) > uint64(len(r.b)) {
				continue
			}
			val = r.b[p : p+total]
		}
		out[tag(r.order.Uint16(e))] = entry{typ, count, val}
	}
	return out, r.order.Uint32(r.b[base+n*12:])
}

func (r *reader) long(e entry) (uint32, bool) {
	switch {
	case e.typ == 4 && len(e.value) >= 4:
		return r.order.Uint32(e.value), true
	case e.typ == 3 && len(e.value) >= 2:
		return uint32(r.order.Uint16(e.value)), true
	}
	return 0, false
}

func (r *reader) rationals(e entry) []float64 {
	if e.typ != 5 && e.typ != 10 {
		return nil
	}
	var out []float64
	for i := 0; i+8 <= len(e.value); i += 8 {
		num, den := r.order.Uint32(e.value[i:]), r.order.Uint32(e.value[i+4:])
		if den == 0 {
			return nil
		}
		if e.typ == 10 {
			out = append(out, float64(int32(num))/float64(int32(den)))
		} else {
			out = append(out, float64(num)/float64(den))
		}
	}
	return out
}

func ascii(e entry) string {
	if e.typ != 2 && e.typ != 7 && e.typ != 1 {
		return ""
	}
	s := string(e.value)
	if i := strings.IndexByte(s, 0); i >= 0 && e.typ == 2 {
		s = s[:i]
	}
	return clean(s)
}

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == 0 || r == '�' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// userComment strips the 8-byte character-code prefix from EXIF UserComment.
func userComment(e entry) string {
	if len(e.value) < 8 {
		return ""
	}
	code, body := string(e.value[:8]), e.value[8:]
	if strings.HasPrefix(code, "UNICODE") {
		return utf16le(body)
	}
	return clean(string(body))
}

func utf16le(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return clean(string(utf16.Decode(u)))
}

func (r *reader) fill(m *Meta, ifd map[tag]entry) {
	text := map[tag]*string{
		tagDescription: &m.Description, tagMake: &m.Make, tagModel: &m.Model,
		tagSoftware: &m.Software, tagDateTime: &m.DateTime, tagArtist: &m.Artist,
		tagHostComputer: &m.HostComputer, tagCopyright: &m.Copyright,
		tagDTOriginal: &m.DateTimeOriginal, tagDTDigitized: &m.DateTimeDigitized,
		tagOffsetTime: &m.OffsetTime, tagUniqueID: &m.UniqueID, tagOwner: &m.Owner,
		tagBodySerial: &m.BodySerial, tagLensModel: &m.LensModel, tagLensSerial: &m.LensSerial,
	}
	for t, dst := range text {
		if e, ok := ifd[t]; ok {
			if v := ascii(e); v != "" {
				*dst = v
			}
		}
	}
	xp := map[tag]*string{tagXPAuthor: &m.XPAuthor, tagXPComment: &m.XPComment, tagXPSubject: &m.XPSubject}
	for t, dst := range xp {
		if e, ok := ifd[t]; ok && e.typ == 1 {
			*dst = utf16le(e.value)
		}
	}
	if e, ok := ifd[tagUserComment]; ok {
		m.UserComment = userComment(e)
	}
	if v, ok := r.long(ifd[tagOrientation]); ok {
		m.Orientation = int(v)
	}
}

func (r *reader) gps(ifd map[tag]entry) *GPS {
	lat, lon := r.rationals(ifd[gpsLat]), r.rationals(ifd[gpsLon])
	if len(lat) != 3 || len(lon) != 3 {
		return nil
	}
	g := &GPS{Lat: dms(lat), Lon: dms(lon)}
	if ref := ascii(ifd[gpsLatRef]); ref == "S" {
		g.Lat = -g.Lat
	}
	if ref := ascii(ifd[gpsLonRef]); ref == "W" {
		g.Lon = -g.Lon
	}
	if g.Lat == 0 && g.Lon == 0 || math.Abs(g.Lat) > 90 || math.Abs(g.Lon) > 180 {
		return nil
	}
	if alt := r.rationals(ifd[gpsAlt]); len(alt) == 1 {
		a := alt[0]
		if e := ifd[gpsAltRef]; len(e.value) == 1 && e.value[0] == 1 {
			a = -a
		}
		g.Alt = &a
	}
	return g
}

func dms(v []float64) float64 { return v[0] + v[1]/60 + v[2]/3600 }
