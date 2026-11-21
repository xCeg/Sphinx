package bmff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/xmp"
)

var xmpUUID = []byte{0xBE, 0x7A, 0xCF, 0xCB, 0x97, 0xA9, 0x42, 0xE8, 0x9C, 0x71, 0x99, 0x94, 0x91, 0xE3, 0xAF, 0xAC}

func Inspect(f []byte) ([]leak.Finding, error) {
	if Brand(f) == "" {
		return nil, errNotBMFF
	}
	if IsHEIF(f) {
		return inspectHEIF(f), nil
	}
	return inspectVideo(f), nil
}

func inspectHEIF(f []byte) []leak.Finding {
	var meta *exif.Meta
	packet := xmp.Packet{}
	for _, it := range heifItems(f) {
		switch {
		case it.typ == "Exif":
			meta, _ = exif.Parse(exifPayload(it.bytes(f)))
		case it.typ == "mime" && strings.Contains(it.mime, "xml"):
			packet = xmp.Parse(it.bytes(f))
		}
	}
	return photo.Findings(meta, packet)
}

// tags collects QuickTime and iTunes-style metadata: the classic ©-prefixed
// user-data atoms, Apple's keys/ilst pairs, the 3GPP loci box and XMP.
type tags struct {
	values  map[string]string
	xmp     xmp.Packet
	created time.Time
}

var (
	iso6709 = regexp.MustCompile(`^([+-]\d{1,2}(?:\.\d+)?)([+-]\d{1,3}(?:\.\d+)?)([+-]\d+(?:\.\d+)?)?`)
	keyName = map[string]string{
		"\xa9xyz": "location", "\xa9mak": "make", "\xa9mod": "model", "\xa9swr": "software",
		"\xa9day": "date", "\xa9aut": "author", "\xa9ART": "author", "\xa9nam": "title", "\xa9cmt": "comment",
		"\xa9des": "description", "\xa9too": "encoder", "\xa9wrt": "author",
	}
)

func readTags(f []byte) tags {
	t := tags{values: map[string]string{}, xmp: xmp.Packet{}}
	var keys []string
	walk(f, 0, len(f), nil, 0, func(b *box) {
		d := b.data(f)
		switch {
		case b.typ == "mvhd" && len(d) >= 12:
			var secs uint64
			if d[0] == 1 && len(d) >= 20 {
				secs = binary.BigEndian.Uint64(d[4:])
			} else {
				secs = uint64(binary.BigEndian.Uint32(d[4:]))
			}
			if secs > 0 {
				t.created = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(secs) * time.Second)
			}
		case b.typ == "keys" && len(d) >= 8:
			keys = keys[:0]
			n := int(binary.BigEndian.Uint32(d[4:]))
			for p := 8; p+8 <= len(d) && len(keys) < n; {
				size := int(binary.BigEndian.Uint32(d[p:]))
				if size < 8 || p+size > len(d) {
					break
				}
				keys = append(keys, string(d[p+8:p+size]))
				p += size
			}
		case b.parent != nil && b.parent.typ == "ilst":
			name := keyName[b.typ]
			if idx := int(binary.BigEndian.Uint32(f[b.start+4:])); name == "" && idx >= 1 && idx <= len(keys) {
				name = strings.TrimPrefix(keys[idx-1], "com.apple.quicktime.")
			}
			if v := dataValue(f, b); name != "" && v != "" {
				t.values[name] = v
			}
		case b.parent != nil && b.parent.typ == "udta" && keyName[b.typ] != "":
			if v := udtaString(d); v != "" {
				t.values[keyName[b.typ]] = v
			}
		case b.typ == "loci":
			if v := loci(d); v != "" {
				t.values["location"] = v
			}
		case b.typ == "XMP_":
			t.xmp = xmp.Parse(d)
		case b.typ == "uuid" && bytes.Equal(f[b.start+8:b.start+24], xmpUUID):
			t.xmp = xmp.Parse(d)
		}
	})
	return t
}

// dataValue reads the text of the "data" box inside an ilst entry.
func dataValue(f []byte, b *box) string {
	var out string
	walk(f, b.body, b.end, b, 0, func(c *box) {
		d := c.data(f)
		if c.typ == "data" && len(d) > 8 {
			out = strings.TrimSpace(string(d[8:]))
		}
	})
	return printable(out)
}

// udtaString reads a QuickTime international text atom: a 16-bit length, a
// 16-bit language code, then the text.
func udtaString(d []byte) string {
	if len(d) < 4 {
		return ""
	}
	n := int(binary.BigEndian.Uint16(d))
	if 4+n > len(d) {
		n = len(d) - 4
	}
	return printable(strings.TrimSpace(string(d[4 : 4+n])))
}

func printable(s string) string {
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			return ""
		}
	}
	return s
}

// loci reads a 3GPP location box: version, language, a name, a role, then
// longitude, latitude and altitude as 16.16 fixed point.
func loci(d []byte) string {
	if len(d) < 6 {
		return ""
	}
	p := 6
	i := bytes.IndexByte(d[p:], 0)
	if i < 0 {
		return ""
	}
	p += i + 2
	if p+12 > len(d) {
		return ""
	}
	fixed := func(b []byte) float64 { return float64(int32(binary.BigEndian.Uint32(b))) / 65536 }
	lon, lat := fixed(d[p:]), fixed(d[p+4:])
	if lat == 0 && lon == 0 {
		return ""
	}
	return fmt.Sprintf("%+.4f%+.4f/", lat, lon)
}

func parseISO6709(s string) *exif.GPS {
	m := iso6709.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	lat, _ := strconv.ParseFloat(m[1], 64)
	lon, _ := strconv.ParseFloat(m[2], 64)
	if (lat == 0 && lon == 0) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return nil
	}
	g := &exif.GPS{Lat: lat, Lon: lon}
	if m[3] != "" {
		if alt, err := strconv.ParseFloat(m[3], 64); err == nil {
			g.Alt = &alt
		}
	}
	return g
}

func inspectVideo(f []byte) []leak.Finding {
	t := readTags(f)
	var out []leak.Finding
	loc := t.values["location"]
	if loc == "" {
		loc = t.values["location.ISO6709"]
	}
	if g := parseISO6709(loc); g != nil {
		out = append(out, leak.GPSLocation.Find("The video records the exact place it was filmed: "+g.String()+".", g.MapURL()).At("QuickTime metadata"))
	}
	var people []string
	for _, k := range []string{"author", "artist", "copyright", "creator"} {
		if v := t.values[k]; v != "" {
			people = append(people, v)
		}
	}
	people = append(people, t.xmp["dc:creator"]...)
	if len(people) > 0 {
		out = append(out, leak.PersonalNames.Find("The video names the person who made it.", strings.Join(people, "; ")).At("QuickTime metadata"))
	}
	var notes []string
	for _, k := range []string{"title", "comment", "description"} {
		if v := t.values[k]; v != "" {
			notes = append(notes, strconv.Quote(photo.Truncate(v, 120)))
		}
	}
	if len(notes) > 0 {
		out = append(out, leak.EmbeddedText.Find("The video carries a title or comments.", strings.Join(notes, "; ")).At("QuickTime metadata"))
	}
	var when []string
	if !t.created.IsZero() && t.created.Year() > 1970 {
		when = append(when, "recorded "+t.created.Format("2006-01-02 15:04:05 UTC"))
	}
	if v := firstOf(t.values["creationdate"], t.values["creation_time"], t.values["date"]); v != "" {
		when = append(when, "creation date "+v)
	}
	if len(when) > 0 {
		out = append(out, leak.Timestamps.Find("The video records when it was filmed.", strings.Join(when, "; ")).At("QuickTime metadata"))
	}
	var trail []string
	for _, k := range []string{"make", "model", "software", "encoder"} {
		if v := t.values[k]; v != "" {
			trail = append(trail, k+" "+v)
		}
	}
	if len(trail) > 0 {
		out = append(out, leak.SoftwareTrail.Find("The video names the device and software that produced it.", strings.Join(trail, "; ")).At("QuickTime metadata"))
	}
	return out
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// sealedTypes are the boxes that carry nothing a player needs. Sealing
// renames them to "free", the standard type for space to be ignored, so
// that no offset in the file moves.
var sealedTypes = map[string]bool{
	"loci": true, "XMP_": true, "\xa9xyz": true, "\xa9mak": true, "\xa9mod": true, "\xa9swr": true,
	"\xa9day": true, "\xa9aut": true, "\xa9ART": true, "\xa9nam": true, "\xa9cmt": true, "\xa9des": true,
	"\xa9too": true, "\xa9wrt": true,
}

// Seal blanks metadata in place. In a video, metadata boxes become free
// space and track and movie creation times are zeroed. In a HEIF photo,
// the Exif item keeps its length but holds an empty TIFF structure, and
// XMP items are zeroed. Nothing is moved, so every offset stays valid.
func Seal(f []byte) ([]byte, error) {
	if Brand(f) == "" {
		return nil, errNotBMFF
	}
	out := append([]byte(nil), f...)
	if IsHEIF(f) {
		for _, it := range heifItems(f) {
			switch {
			case it.typ == "Exif":
				blankExif(out, it)
			case it.typ == "mime":
				for _, e := range it.extents {
					clear(out[e[0]:e[1]])
				}
			}
		}
		return out, nil
	}
	walk(f, 0, len(f), nil, 0, func(b *box) {
		switch {
		case sealedTypes[b.typ],
			b.typ == "meta" && b.parent != nil && b.parent.typ == "udta",
			b.typ == "uuid" && bytes.Equal(f[b.start+8:b.start+24], xmpUUID):
			copy(out[b.start+4:], "free")
		case b.typ == "mvhd" || b.typ == "tkhd" || b.typ == "mdhd":
			d := out[b.body:b.end]
			switch {
			case len(d) >= 20 && d[0] == 1:
				clear(d[4:20])
			case len(d) >= 12:
				clear(d[4:12])
			}
		}
	})
	return out, nil
}

func blankExif(out []byte, it item) {
	empty := []byte{0, 0, 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	var pos []int
	for _, e := range it.extents {
		for i := e[0]; i < e[1]; i++ {
			pos = append(pos, i)
		}
	}
	for i, p := range pos {
		if i < len(empty) {
			out[p] = empty[i]
		} else {
			out[p] = 0
		}
	}
}
