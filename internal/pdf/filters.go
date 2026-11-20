package pdf

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
)

const maxDecoded = 256 << 20

// decode applies a stream's filters. Damaged Flate data is common in
// real files; whatever decodes before the damage is kept, as viewers do.
func (d *Document) decode(s *Stream) ([]byte, error) {
	data := s.Raw
	filters := d.list(s.Dict["Filter"])
	params := d.list(s.Dict["DecodeParms"])
	for i, f := range filters {
		name, _ := d.Resolve(f).(Name)
		var p Dict
		if i < len(params) {
			p, _ = d.Resolve(params[i]).(Dict)
		}
		var err error
		switch name {
		case "FlateDecode", "Fl":
			data, err = inflate(data)
			if err == nil {
				data, err = predict(data, p, d)
			}
		case "LZWDecode", "LZW":
			early := 1
			if v, ok := num(d.Resolve(p["EarlyChange"])); ok {
				early = int(v)
			}
			data = lzw(data, early)
			data, err = predict(data, p, d)
		case "ASCIIHexDecode", "AHx":
			data = asciiHex(data)
		case "ASCII85Decode", "A85":
			data = ascii85(data)
		case "RunLengthDecode", "RL":
			data = runLength(data)
		default:
			return nil, fmt.Errorf("pdf: unsupported filter %s", name)
		}
		if err != nil {
			return data, err
		}
	}
	return data, nil
}

func (d *Document) list(o Object) Array {
	switch v := d.Resolve(o).(type) {
	case Array:
		return v
	case nil:
		return nil
	default:
		return Array{v}
	}
}

func inflate(b []byte) ([]byte, error) {
	var r io.Reader
	if zr, err := zlib.NewReader(bytes.NewReader(b)); err == nil {
		r = zr
	} else {
		r = flate.NewReader(bytes.NewReader(b))
	}
	out, err := io.ReadAll(io.LimitReader(r, maxDecoded))
	if len(out) > 0 {
		return out, nil
	}
	return out, err
}

// predict undoes PNG (10-15) and TIFF (2) predictors, used mostly by
// cross-reference and object streams.
func predict(b []byte, p Dict, d *Document) ([]byte, error) {
	get := func(k Name, def int) int {
		if v, ok := num(d.Resolve(p[k])); ok {
			return int(v)
		}
		return def
	}
	pred := get("Predictor", 1)
	if pred < 2 {
		return b, nil
	}
	colors, bpc, cols := get("Colors", 1), get("BitsPerComponent", 8), get("Columns", 1)
	bpp := max(1, colors*bpc/8)
	row := (colors*bpc*cols + 7) / 8
	if row <= 0 || row > 1<<20 {
		return nil, fmt.Errorf("pdf: bad predictor parameters")
	}
	if pred == 2 {
		out := append([]byte(nil), b...)
		for r := 0; r+row <= len(out); r += row {
			for i := bpp; i < row; i++ {
				out[r+i] += out[r+i-bpp]
			}
		}
		return out, nil
	}
	var out []byte
	prev := make([]byte, row)
	for p := 0; p+row+1 <= len(b); p += row + 1 {
		ft, line := b[p], append([]byte(nil), b[p+1:p+1+row]...)
		for i := range line {
			var a, c byte
			if i >= bpp {
				a, c = line[i-bpp], prev[i-bpp]
			}
			up := prev[i]
			switch ft {
			case 1:
				line[i] += a
			case 2:
				line[i] += up
			case 3:
				line[i] += byte((int(a) + int(up)) / 2)
			case 4:
				line[i] += paeth(a, up, c)
			}
		}
		out = append(out, line...)
		prev = line
	}
	return out, nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := iabs(p-int(a)), iabs(p-int(b)), iabs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func asciiHex(b []byte) []byte {
	l := lexer{b: append(append([]byte{'<'}, b...), '>')}
	s, _ := l.hex()
	return s.(String)
}

func ascii85(b []byte) []byte {
	var out []byte
	var group [5]byte
	n := 0
	flush := func(count int) {
		var v uint32
		for i := 0; i < 5; i++ {
			c := byte('u')
			if i < count {
				c = group[i]
			}
			v = v*85 + uint32(c-'!')
		}
		w := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		out = append(out, w[:count-1]...)
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '~':
			if n > 1 {
				flush(n)
			}
			return out
		case c == 'z' && n == 0:
			out = append(out, 0, 0, 0, 0)
		case c >= '!' && c <= 'u':
			group[n] = c
			n++
			if n == 5 {
				flush(5)
				n = 0
			}
		}
	}
	if n > 1 {
		flush(n)
	}
	return out
}

func runLength(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		n := int(b[i])
		i++
		switch {
		case n == 128:
			return out
		case n < 128:
			end := min(len(b), i+n+1)
			out = append(out, b[i:end]...)
			i = end
		case i < len(b):
			out = append(out, bytes.Repeat(b[i:i+1], 257-n)...)
			i++
		}
	}
	return out
}

// lzw implements the PDF variant, whose code width grows one code early
// unless EarlyChange is 0; compress/lzw does not support that.
func lzw(b []byte, early int) []byte {
	var out []byte
	dict := make([][]byte, 258, 4096)
	reset := func() {
		dict = dict[:258]
		for i := 0; i < 256; i++ {
			dict[i] = []byte{byte(i)}
		}
	}
	reset()
	width := 9
	var prev []byte
	var acc uint32
	bits := 0
	for _, c := range b {
		acc = acc<<8 | uint32(c)
		bits += 8
		for bits >= width {
			code := int(acc >> uint(bits-width) & (1<<width - 1))
			bits -= width
			switch {
			case code == 256:
				reset()
				width, prev = 9, nil
				continue
			case code == 257:
				return out
			}
			var entry []byte
			switch {
			case code < len(dict):
				entry = dict[code]
			case code == len(dict) && prev != nil:
				entry = append(append([]byte(nil), prev...), prev[0])
			default:
				return out
			}
			out = append(out, entry...)
			if len(out) > maxDecoded {
				return out
			}
			if prev != nil && len(dict) < 4096 {
				dict = append(dict, append(append([]byte(nil), prev...), entry[0]))
			}
			prev = entry
			if len(dict)+early >= 1<<width && width < 12 {
				width++
			}
		}
	}
	return out
}
