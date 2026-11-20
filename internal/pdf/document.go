package pdf

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
)

type def struct {
	ref    Ref
	offset int
	obj    Object
}

// File is a scanned PDF: every object definition in file order, every
// trailer, and the end-of-file markers that delimit incremental updates.
type File struct {
	data     []byte
	defs     []def
	trailers []def
	eofs     []int
}

// Document is a view of the file as it stood at one revision.
type Document struct {
	file    *File
	limit   int
	objects map[int]Object
	trailer Dict
	decoded map[*Stream][]byte
}

var (
	ErrNotPDF    = errors.New("not a PDF file")
	ErrEncrypted = errors.New("the PDF is encrypted")
	objHeader    = regexp.MustCompile(`(\d{1,10})\s+(\d{1,5})\s+obj\b`)
)

func Parse(b []byte) (*File, error) {
	start := bytes.Index(b[:min(len(b), 1024)], []byte("%PDF-"))
	if start < 0 {
		return nil, ErrNotPDF
	}
	f := &File{data: b}
	var skips [][2]int
	pos := start
	for {
		loc := objHeader.FindSubmatchIndex(b[pos:])
		if loc == nil {
			break
		}
		at := pos + loc[0]
		n, _ := strconv.Atoi(string(b[pos+loc[2] : pos+loc[3]]))
		g, _ := strconv.Atoi(string(b[pos+loc[4] : pos+loc[5]]))
		l := &lexer{b: b, pos: pos + loc[1]}
		obj, err := l.object()
		if err != nil {
			pos += loc[1]
			continue
		}
		if dict, ok := obj.(Dict); ok {
			save := l.pos
			if t, _ := l.token(); t == Keyword("stream") {
				s, end := readStream(b, l.pos, dict)
				obj = s
				skips = append(skips, [2]int{l.pos, end})
				l.pos = end
			} else {
				l.pos = save
			}
		}
		d := def{Ref{n, g}, at, obj}
		f.defs = append(f.defs, d)
		if s, ok := obj.(*Stream); ok && s.Dict.Name("Type") == "XRef" {
			f.trailers = append(f.trailers, def{d.ref, at, s.Dict})
		}
		pos = max(l.pos, pos+loc[1])
	}
	f.findTrailers(start, skips)
	return f, nil
}

func readStream(b []byte, pos int, dict Dict) (*Stream, int) {
	if pos < len(b) && b[pos] == '\r' {
		pos++
	}
	if pos < len(b) && b[pos] == '\n' {
		pos++
	}
	if n, ok := dict["Length"].(int); ok && n >= 0 && pos+n <= len(b) {
		rest := bytes.TrimLeft(b[pos+n:min(len(b), pos+n+32)], "\r\n \t")
		if bytes.HasPrefix(rest, []byte("endstream")) {
			end := pos + n
			return &Stream{Dict: dict, Raw: b[pos:end]}, end
		}
	}
	i := bytes.Index(b[pos:], []byte("endstream"))
	if i < 0 {
		return &Stream{Dict: dict, Raw: b[pos:]}, len(b)
	}
	raw := bytes.TrimSuffix(bytes.TrimSuffix(b[pos:pos+i], []byte("\n")), []byte("\r"))
	return &Stream{Dict: dict, Raw: raw}, pos + i
}

func inside(skips [][2]int, at int) bool {
	for _, s := range skips {
		if at >= s[0] && at < s[1] {
			return true
		}
	}
	return false
}

func (f *File) findTrailers(start int, skips [][2]int) {
	for p := start; ; {
		i := bytes.Index(f.data[p:], []byte("trailer"))
		if i < 0 {
			break
		}
		at := p + i
		p = at + 7
		if inside(skips, at) {
			continue
		}
		l := &lexer{b: f.data, pos: p}
		if d, err := l.object(); err == nil {
			if dict, ok := d.(Dict); ok {
				f.trailers = append(f.trailers, def{offset: at, obj: dict})
			}
		}
	}
	for p := start; ; {
		i := bytes.Index(f.data[p:], []byte("%%EOF"))
		if i < 0 {
			break
		}
		at := p + i
		p = at + 5
		if !inside(skips, at) {
			f.eofs = append(f.eofs, p)
		}
	}
}

// Revisions returns the byte offsets at which each saved version of the
// document ends, oldest first. The final entry is the whole file. The
// first-page section of a linearized file has its own end marker but is
// not a version anyone saved, so it is skipped, as are markers with no new
// objects before them.
func (f *File) Revisions() []int {
	var out []int
	defIdx := 0
	for _, e := range f.eofs {
		added := 0
		for defIdx < len(f.defs) && f.defs[defIdx].offset < e {
			added++
			defIdx++
		}
		if added == 0 && len(out) > 0 {
			continue
		}
		out = append(out, e)
	}
	if len(out) > 1 && f.linearized() {
		out = out[1:]
	}
	if len(out) == 0 || defIdx < len(f.defs) {
		out = append(out, len(f.data))
	}
	return out
}

func (f *File) linearized() bool {
	if len(f.defs) == 0 {
		return false
	}
	d, ok := f.defs[0].obj.(Dict)
	return ok && d["Linearized"] != nil
}

// At returns the document as it stood when the file ended at limit.
func (f *File) At(limit int) *Document {
	d := &Document{file: f, limit: limit, objects: map[int]Object{}, trailer: Dict{}, decoded: map[*Stream][]byte{}}
	for _, x := range f.defs {
		if x.offset >= limit {
			break
		}
		d.objects[x.ref.Num] = x.obj
		if s, ok := x.obj.(*Stream); ok && s.Dict.Name("Type") == "ObjStm" {
			d.unpack(s)
		}
	}
	for _, x := range f.defs {
		if x.offset < limit {
			if _, ok := x.obj.(*Stream); !ok || x.obj.(*Stream).Dict.Name("Type") != "ObjStm" {
				d.objects[x.ref.Num] = x.obj
			}
		}
	}
	for _, t := range f.trailers {
		if t.offset >= limit {
			continue
		}
		for k, v := range t.obj.(Dict) {
			d.trailer[k] = v
		}
	}
	return d
}

func (f *File) Final() *Document { return f.At(len(f.data)) }

// unpack loads the objects compressed inside an object stream. Objects
// defined directly in the file are applied afterwards and win.
func (d *Document) unpack(s *Stream) {
	data, err := d.decode(s)
	if err != nil && len(data) == 0 {
		return
	}
	n, _ := num(s.Dict["N"])
	first, _ := num(s.Dict["First"])
	l := &lexer{b: data}
	type pair struct{ num, off int }
	var pairs []pair
	for i := 0; i < int(n) && i < 1<<20; i++ {
		a, err1 := l.token()
		b, err2 := l.token()
		an, ok1 := a.(int)
		bn, ok2 := b.(int)
		if err1 != nil || err2 != nil || !ok1 || !ok2 {
			break
		}
		pairs = append(pairs, pair{an, bn})
	}
	for _, p := range pairs {
		at := int(first) + p.off
		if at < 0 || at >= len(data) {
			continue
		}
		ol := &lexer{b: data, pos: at}
		if o, err := ol.object(); err == nil {
			d.objects[p.num] = o
		}
	}
}

// Resolve follows references, at most a few levels deep.
func (d *Document) Resolve(o Object) Object {
	for range 16 {
		r, ok := o.(Ref)
		if !ok {
			return o
		}
		o = d.objects[r.Num]
	}
	return nil
}

func (d *Document) Dict(o Object) Dict {
	switch v := d.Resolve(o).(type) {
	case Dict:
		return v
	case *Stream:
		return v.Dict
	}
	return nil
}

func (d *Document) Num(o Object) (float64, bool) { return num(d.Resolve(o)) }

// Data returns a stream's decoded content.
func (d *Document) Data(o Object) []byte {
	s, ok := d.Resolve(o).(*Stream)
	if !ok {
		return nil
	}
	if b, ok := d.decoded[s]; ok {
		return b
	}
	b, _ := d.decode(s)
	d.decoded[s] = b
	return b
}

func (d *Document) Trailer() Dict { return d.trailer }

func (d *Document) Encrypted() bool { return d.trailer["Encrypt"] != nil }

// Root returns the catalog, falling back to any object that declares
// itself one when the trailer is missing or damaged.
func (d *Document) Root() Dict {
	if r := d.Dict(d.trailer["Root"]); r != nil {
		return r
	}
	best := -1
	for n, o := range d.objects {
		if dict, ok := o.(Dict); ok && dict.Name("Type") == "Catalog" && n > best {
			best = n
		}
	}
	if best < 0 {
		return nil
	}
	return d.objects[best].(Dict)
}

func (d *Document) Info() Dict { return d.Dict(d.trailer["Info"]) }

// Page is a leaf of the page tree with its inherited attributes resolved.
type Page struct {
	Dict      Dict
	Resources Dict
	MediaBox  [4]float64
}

func (d *Document) Pages() []Page {
	var out []Page
	seen := map[Ref]bool{}
	var walk func(o Object, res Dict, box [4]float64, depth int)
	walk = func(o Object, res Dict, box [4]float64, depth int) {
		if r, ok := o.(Ref); ok {
			if seen[r] {
				return
			}
			seen[r] = true
		}
		node := d.Dict(o)
		if node == nil || depth > 64 || len(out) > 100000 {
			return
		}
		if r := d.Dict(node["Resources"]); r != nil {
			res = r
		}
		if b, ok := d.rect(node["MediaBox"]); ok {
			box = b
		}
		kids, isTree := d.Resolve(node["Kids"]).(Array)
		if node.Name("Type") == "Page" || !isTree {
			out = append(out, Page{Dict: node, Resources: res, MediaBox: box})
			return
		}
		for _, k := range kids {
			walk(k, res, box, depth+1)
		}
	}
	if root := d.Root(); root != nil {
		walk(root["Pages"], nil, [4]float64{0, 0, 612, 792}, 0)
	}
	return out
}

func (d *Document) rect(o Object) ([4]float64, bool) {
	a, ok := d.Resolve(o).(Array)
	if !ok || len(a) != 4 {
		return [4]float64{}, false
	}
	var r [4]float64
	for i := range r {
		v, ok := d.Num(a[i])
		if !ok {
			return r, false
		}
		r[i] = v
	}
	if r[0] > r[2] {
		r[0], r[2] = r[2], r[0]
	}
	if r[1] > r[3] {
		r[1], r[3] = r[3], r[1]
	}
	return r, true
}

// Bytes returns the file content up to the end of this revision.
func (d *Document) Bytes() []byte { return d.file.data[:d.limit] }
