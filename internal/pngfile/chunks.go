// Package pngfile inspects, reveals and seals PNG images.
package pngfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

var signature = []byte("\x89PNG\r\n\x1a\n")

type chunk struct {
	Type   string
	Data   []byte
	Offset int
}

func (c chunk) end() int { return c.Offset + 12 + len(c.Data) }

type header struct {
	Width, Height int
	BitDepth      int
	ColorType     int
	Interlace     int
}

// channels per color type, from the PNG specification, table 11.1.
var channels = map[int]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}

func (h header) bytesPerPixel() int {
	return max(1, channels[h.ColorType]*h.BitDepth/8)
}

type file struct {
	header   header
	chunks   []chunk
	trailing []byte
	end      int
}

var errNotPNG = errors.New("not a PNG file")

func parse(b []byte) (*file, error) {
	if !bytes.HasPrefix(b, signature) {
		return nil, errNotPNG
	}
	f := &file{}
	for p := len(signature); ; {
		if p+12 > len(b) {
			return nil, fmt.Errorf("PNG ends early at byte %d, before its IEND chunk", p)
		}
		n := int(binary.BigEndian.Uint32(b[p:]))
		if n > len(b)-p-12 {
			return nil, fmt.Errorf("PNG chunk at byte %d claims %d bytes, more than the file holds", p, n)
		}
		c := chunk{Type: string(b[p+4 : p+8]), Data: b[p+8 : p+8+n], Offset: p}
		f.chunks = append(f.chunks, c)
		p = c.end()
		if c.Type == "IHDR" && n >= 13 {
			d := c.Data
			f.header = header{
				Width: int(binary.BigEndian.Uint32(d)), Height: int(binary.BigEndian.Uint32(d[4:])),
				BitDepth: int(d[8]), ColorType: int(d[9]), Interlace: int(d[12]),
			}
		}
		if c.Type == "IEND" {
			f.end = p
			f.trailing = b[p:]
			return f, nil
		}
	}
}

func validCRC(b []byte, off, n int) bool {
	if off+12+n > len(b) {
		return false
	}
	want := binary.BigEndian.Uint32(b[off+8+n:])
	return crc32.ChecksumIEEE(b[off+4:off+8+n]) == want
}

func appendChunk(out []byte, typ string, data []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	start := len(out)
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
}
