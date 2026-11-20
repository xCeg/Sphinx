package pdf

import (
	"strings"
	"unicode/utf16"
)

// font knows how to split a string into character codes, what text each
// code stands for, and how wide each glyph is.
type font struct {
	codeLen   []codeRange
	toUnicode map[uint32]string
	simple    [256]string
	composite bool
	widths    map[uint32]float64
	missing   float64
	scale     float64
	ascent    float64
	descent   float64
}

type codeRange struct {
	n      int
	lo, hi uint32
}

type glyph struct {
	code  uint32
	text  string
	width float64
	space bool
}

func (d *Document) loadFont(o Object) *font {
	fd := d.Dict(o)
	f := &font{widths: map[uint32]float64{}, scale: 0.001, missing: 500, ascent: 0.8, descent: -0.2}
	if fd == nil {
		f.simple = baseEncoding("WinAnsiEncoding")
		return f
	}
	sub := fd.Name("Subtype")
	desc := fd
	if sub == "Type0" {
		f.composite = true
		if kids, ok := d.Resolve(fd["DescendantFonts"]).(Array); ok && len(kids) > 0 {
			desc = d.Dict(kids[0])
		}
		f.cidWidths(d, desc)
		f.codeLen = []codeRange{{2, 0, 0xFFFF}}
	} else {
		f.simpleEncoding(d, fd)
		f.simpleWidths(d, fd)
	}
	if sub == "Type3" {
		if m, ok := d.Resolve(fd["FontMatrix"]).(Array); ok && len(m) == 6 {
			if v, ok := d.Num(m[0]); ok && v != 0 {
				f.scale = v
			}
		}
	}
	if desc != nil {
		if fdesc := d.Dict(desc["FontDescriptor"]); fdesc != nil {
			if v, ok := d.Num(fdesc["Ascent"]); ok && v > 0 {
				f.ascent = v / 1000
			}
			if v, ok := d.Num(fdesc["Descent"]); ok && v < 0 {
				f.descent = v / 1000
			}
			if v, ok := d.Num(fdesc["MissingWidth"]); ok && v > 0 {
				f.missing = v
			}
		}
	}
	if cm := d.Data(fd["ToUnicode"]); len(cm) > 0 {
		f.toUnicode, f.codeLen = parseCMap(cm, f.codeLen)
	}
	return f
}

func (f *font) simpleEncoding(d *Document, fd Dict) {
	enc := d.Resolve(fd["Encoding"])
	switch e := enc.(type) {
	case Name:
		f.simple = baseEncoding(e)
	case Dict:
		f.simple = baseEncoding(e.Name("BaseEncoding"))
		if diffs, ok := d.Resolve(e["Differences"]).(Array); ok {
			code := 0
			for _, x := range diffs {
				switch v := d.Resolve(x).(type) {
				case int:
					code = v
				case Name:
					if code >= 0 && code < 256 {
						if t := glyphText(string(v)); t != "" {
							f.simple[code] = t
						}
					}
					code++
				}
			}
		}
	default:
		f.simple = baseEncoding("WinAnsiEncoding")
	}
}

func (f *font) simpleWidths(d *Document, fd Dict) {
	first, _ := d.Num(fd["FirstChar"])
	if ws, ok := d.Resolve(fd["Widths"]).(Array); ok {
		for i, w := range ws {
			if v, ok := d.Num(w); ok {
				f.widths[uint32(int(first)+i)] = v
			}
		}
		return
	}
	if strings.Contains(string(fd.Name("BaseFont")), "Courier") {
		f.missing = 600
	}
}

func (f *font) cidWidths(d *Document, desc Dict) {
	if desc == nil {
		return
	}
	f.missing = 1000
	if v, ok := d.Num(desc["DW"]); ok {
		f.missing = v
	}
	w, _ := d.Resolve(desc["W"]).(Array)
	for i := 0; i < len(w); {
		start, ok := d.Num(w[i])
		if !ok || i+1 >= len(w) {
			return
		}
		if list, ok := d.Resolve(w[i+1]).(Array); ok {
			for j, x := range list {
				if v, ok := d.Num(x); ok {
					f.widths[uint32(int(start)+j)] = v
				}
			}
			i += 2
			continue
		}
		if i+2 >= len(w) {
			return
		}
		end, ok1 := d.Num(w[i+1])
		v, ok2 := d.Num(w[i+2])
		if ok1 && ok2 && end-start < 1<<16 {
			for c := int(start); c <= int(end); c++ {
				f.widths[uint32(c)] = v
			}
		}
		i += 3
	}
}

func (f *font) glyphs(s []byte) []glyph {
	var out []glyph
	for i := 0; i < len(s); {
		n := f.lengthAt(s[i:])
		var code uint32
		for _, c := range s[i : i+n] {
			code = code<<8 | uint32(c)
		}
		i += n
		g := glyph{code: code, width: f.missing}
		if w, ok := f.widths[code]; ok {
			g.width = w
		}
		g.width *= f.scale
		if t, ok := f.toUnicode[code]; ok {
			g.text = t
		} else if !f.composite {
			g.text = f.simple[code&0xFF]
		}
		g.space = n == 1 && code == 32
		out = append(out, g)
	}
	return out
}

func (f *font) lengthAt(s []byte) int {
	if len(f.codeLen) == 0 {
		return 1
	}
	for n := 1; n <= 4 && n <= len(s); n++ {
		var code uint32
		for _, c := range s[:n] {
			code = code<<8 | uint32(c)
		}
		for _, r := range f.codeLen {
			if r.n == n && code >= r.lo && code <= r.hi {
				return n
			}
		}
	}
	return min(len(s), f.codeLen[0].n)
}

// parseCMap reads the bfchar and bfrange mappings of a ToUnicode CMap, and
// its code space, which decides how many bytes make up one code.
func parseCMap(b []byte, codeLen []codeRange) (map[uint32]string, []codeRange) {
	out := map[uint32]string{}
	var space []codeRange
	l := &lexer{b: b}
	var stack []Object
	code := func(o Object) (uint32, int, bool) {
		s, ok := o.(String)
		if !ok || len(s) == 0 || len(s) > 4 {
			return 0, 0, false
		}
		var v uint32
		for _, c := range s {
			v = v<<8 | uint32(c)
		}
		return v, len(s), true
	}
	for {
		t, err := l.object()
		if err != nil {
			break
		}
		k, isKw := t.(Keyword)
		if !isKw {
			stack = append(stack, t)
			continue
		}
		switch k {
		case "endcodespacerange":
			for i := 0; i+1 < len(stack); i += 2 {
				lo, n, ok1 := code(stack[i])
				hi, _, ok2 := code(stack[i+1])
				if ok1 && ok2 {
					space = append(space, codeRange{n, lo, hi})
				}
			}
		case "endbfchar":
			for i := 0; i+1 < len(stack); i += 2 {
				if c, _, ok := code(stack[i]); ok {
					out[c] = utf16Text(stack[i+1])
				}
			}
		case "endbfrange":
			for i := 0; i+2 < len(stack); i += 3 {
				lo, _, ok1 := code(stack[i])
				hi, _, ok2 := code(stack[i+1])
				if !ok1 || !ok2 || hi < lo || hi-lo > 1<<16 {
					continue
				}
				switch dst := stack[i+2].(type) {
				case String:
					base := []rune(utf16Text(dst))
					if len(base) == 0 {
						continue
					}
					for c := lo; c <= hi; c++ {
						r := append([]rune(nil), base...)
						r[len(r)-1] += rune(c - lo)
						out[c] = string(r)
					}
				case Array:
					for j, x := range dst {
						if lo+uint32(j) > hi {
							break
						}
						out[lo+uint32(j)] = utf16Text(x)
					}
				}
			}
		}
		stack = stack[:0]
	}
	if len(space) == 0 {
		space = codeLen
	}
	return out, space
}

func utf16Text(o Object) string {
	s, ok := o.(String)
	if !ok {
		return ""
	}
	u := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		u = append(u, uint16(s[i])<<8|uint16(s[i+1]))
	}
	return string(utf16.Decode(u))
}
