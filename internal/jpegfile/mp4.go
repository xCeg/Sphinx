package jpegfile

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// video describes an MP4 found inside a photo: how long it runs and
// whether it records sound, which is what makes motion photos sensitive.
type video struct {
	Offset   int
	Size     int
	Seconds  float64
	HasAudio bool
}

func (v video) String() string {
	s := fmt.Sprintf("a %s video", mib(v.Size))
	if v.Seconds > 0 {
		s = fmt.Sprintf("a %.1f-second video (%s)", v.Seconds, mib(v.Size))
	}
	if v.HasAudio {
		s += " with sound"
	}
	return s
}

func mib(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

// findVideo locates an ISO-BMFF file inside b by its leading ftyp box.
func findVideo(b []byte) (video, bool) {
	for p := 0; ; {
		i := bytes.Index(b[p:], []byte("ftyp"))
		if i < 0 {
			return video{}, false
		}
		start := p + i - 4
		p += i + 4
		if start < 0 {
			continue
		}
		n := int(binary.BigEndian.Uint32(b[start:]))
		if n < 8 || n > 256 || start+n > len(b) {
			continue
		}
		v := video{Offset: start, Size: len(b) - start}
		walk(b[start:], &v, 0)
		return v, true
	}
}

func walk(b []byte, v *video, depth int) {
	for p := 0; p+8 <= len(b) && depth < 6; {
		n := int(binary.BigEndian.Uint32(b[p:]))
		typ := string(b[p+4 : p+8])
		hdr := 8
		if n == 1 && p+16 <= len(b) {
			n, hdr = int(binary.BigEndian.Uint64(b[p+8:])), 16
		}
		if n == 0 {
			n = len(b) - p
		}
		if n < hdr || p+n > len(b) {
			return
		}
		body := b[p+hdr : p+n]
		switch typ {
		case "moov", "trak", "mdia":
			walk(body, v, depth+1)
		case "mvhd":
			v.Seconds = duration(body)
		case "hdlr":
			if len(body) >= 12 && string(body[8:12]) == "soun" {
				v.HasAudio = true
			}
		}
		p += n
	}
}

func duration(mvhd []byte) float64 {
	if len(mvhd) < 1 {
		return 0
	}
	if mvhd[0] == 1 && len(mvhd) >= 32 {
		scale := binary.BigEndian.Uint32(mvhd[20:])
		d := binary.BigEndian.Uint64(mvhd[24:])
		if scale > 0 {
			return float64(d) / float64(scale)
		}
	}
	if len(mvhd) >= 20 {
		scale := binary.BigEndian.Uint32(mvhd[12:])
		d := binary.BigEndian.Uint32(mvhd[16:])
		if scale > 0 {
			return float64(d) / float64(scale)
		}
	}
	return 0
}
