package jpegfile

import (
	"bytes"
	"encoding/binary"
)

// iptc reads IPTC-NAA records from a Photoshop APP13 block. Only the
// dataset numbers that name people, places or captions are kept.
func iptc(app13 []byte) map[byte][]string {
	out := map[byte][]string{}
	for p := 0; p+12 <= len(app13); {
		if !bytes.Equal(app13[p:p+4], []byte("8BIM")) {
			return out
		}
		id := binary.BigEndian.Uint16(app13[p+4:])
		nameLen := int(app13[p+6])
		q := p + 7 + nameLen
		if (nameLen+1)%2 == 1 {
			q++
		}
		if q+4 > len(app13) {
			return out
		}
		size := int(binary.BigEndian.Uint32(app13[q:]))
		q += 4
		if size < 0 || q+size > len(app13) {
			return out
		}
		if id == 0x0404 {
			records(app13[q:q+size], out)
		}
		p = q + size + size%2
	}
	return out
}

func records(b []byte, out map[byte][]string) {
	for p := 0; p+5 <= len(b) && b[p] == 0x1C; {
		rec, ds := b[p+1], b[p+2]
		n := int(binary.BigEndian.Uint16(b[p+3:]))
		p += 5
		if n&0x8000 != 0 || p+n > len(b) {
			return
		}
		if rec == 2 {
			if v := string(bytes.TrimSpace(b[p : p+n])); v != "" {
				out[ds] = append(out[ds], v)
			}
		}
		p += n
	}
}

const (
	iptcByline    = 80
	iptcCity      = 90
	iptcSublocal  = 92
	iptcState     = 95
	iptcCountry   = 101
	iptcCopyright = 116
	iptcCaption   = 120
	iptcWriter    = 122
)
