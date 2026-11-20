package pdf

import (
	"bytes"
	"math"
)

type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mul returns m followed by n.
func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m matrix) apply(x, y float64) (float64, float64) {
	return x*m[0] + y*m[2] + m[4], x*m[1] + y*m[3] + m[5]
}

// Box is an axis-aligned rectangle in default user space (points).
type Box struct{ X0, Y0, X1, Y1 float64 }

func (b Box) area() float64 { return math.Max(0, b.X1-b.X0) * math.Max(0, b.Y1-b.Y0) }

func (b Box) intersect(o Box) Box {
	return Box{math.Max(b.X0, o.X0), math.Max(b.Y0, o.Y0), math.Min(b.X1, o.X1), math.Min(b.Y1, o.Y1)}
}

func (b Box) contains(x, y float64) bool {
	return x >= b.X0 && x <= b.X1 && y >= b.Y0 && y <= b.Y1
}

func boxOf(m matrix, x0, y0, x1, y1 float64) Box {
	b := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range [][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		x, y := m.apply(p[0], p[1])
		b.X0, b.Y0 = math.Min(b.X0, x), math.Min(b.Y0, y)
		b.X1, b.Y1 = math.Max(b.X1, x), math.Max(b.Y1, y)
	}
	return b
}

// Glyph is one painted character.
type Glyph struct {
	Text      string
	Box       Box
	Size      float64
	Seq       int
	Space     bool
	Invisible bool
	NewLine   bool
	// Color is the fill colour the glyph is painted with. Stroked is set
	// when the glyph is also outlined, which keeps white text visible.
	Color   [3]float64
	Stroked bool
	// Clipped is set when the glyph lies outside the clipping area in force.
	Clipped bool
	// Layer is set when the glyph belongs to an optional-content layer that
	// is switched off when the document opens.
	Layer bool
}

// Fill is one painted area. Only fills built from rectangles are kept as
// Rect; other shapes (logos, outlined glyphs) cannot hide text in the way
// a redaction box does and would only add noise.
type Fill struct {
	Box    Box
	Seq    int
	Color  [3]float64
	Opaque bool
}

type Canvas struct {
	Glyphs []Glyph
	Fills  []Fill
	// Images are the areas covered by pictures, kept to tell an OCR text
	// layer or white text printed over a photo from text hidden on purpose.
	Images []Fill
	// Grounds are the bounding boxes of every filled shape, rectangular or
	// not, used only to tell whether white text has something dark under it.
	Grounds []Fill
	Page    Box
}

type gstate struct {
	ctm                        matrix
	fill                       [3]float64
	fillSpace                  int
	opaque                     bool
	font                       *font
	size, tc, tw, th, tl, rise float64
	render                     int
	// clip bounds every later fill. textClip is set once text has been
	// shown in a clipping render mode: fills that follow paint the inside
	// of the glyphs, which colours the text instead of hiding it.
	clip     Box
	textClip bool
}

type interp struct {
	doc    *Document
	canvas *Canvas
	fonts  map[Ref]*font
	seq    int
	depth  int
	// lastY tracks the baseline of the previous glyph to mark line breaks.
	lastY float64
	// layers records, for each open marked-content sequence, whether it
	// belongs to a hidden optional-content group.
	layers []bool
	hidden int
}

const maxOps = 5_000_000

// Render interprets a page's content streams and returns what was painted.
func (d *Document) Render(p Page) *Canvas {
	page := Box{p.MediaBox[0], p.MediaBox[1], p.MediaBox[2], p.MediaBox[3]}
	c := &Canvas{Page: page}
	in := &interp{doc: d, canvas: c, fonts: map[Ref]*font{}}
	var content []byte
	for _, s := range d.list(p.Dict["Contents"]) {
		content = append(content, d.Data(s)...)
		content = append(content, '\n')
	}
	gs := gstate{ctm: identity, opaque: true, th: 1, fillSpace: 1, clip: page}
	in.run(content, p.Resources, gs)
	return c
}

func (in *interp) run(content []byte, res Dict, gs gstate) {
	if in.depth > 8 {
		return
	}
	in.depth++
	defer func() { in.depth-- }()

	l := &lexer{b: content}
	var stack []gstate
	var ops []Object
	var tm, tlm matrix
	var path []Box
	var pathPts [][2]float64
	pathRect := true
	clipping := false
	endPath := func() {
		if clipping {
			gs.clip = gs.clip.intersect(pathBounds(path, pathPts))
			clipping = false
		}
		path, pathPts, pathRect = path[:0], pathPts[:0], true
	}
	floats := func(n int) ([]float64, bool) {
		if len(ops) < n {
			return nil, false
		}
		out := make([]float64, n)
		for i, o := range ops[len(ops)-n:] {
			v, ok := num(o)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	}
	for count := 0; count < maxOps; count++ {
		t, err := l.object()
		if err != nil {
			return
		}
		op, isOp := t.(Keyword)
		if !isOp {
			ops = append(ops, t)
			continue
		}
		switch op {
		case "q":
			if len(stack) < 256 {
				stack = append(stack, gs)
			}
		case "Q":
			if len(stack) > 0 {
				gs, stack = stack[len(stack)-1], stack[:len(stack)-1]
			}
		case "cm":
			if v, ok := floats(6); ok {
				gs.ctm = matrix(v).mul(gs.ctm)
			}
		case "g":
			if v, ok := floats(1); ok {
				gs.fill, gs.fillSpace = [3]float64{v[0], v[0], v[0]}, 1
			}
		case "rg":
			if v, ok := floats(3); ok {
				gs.fill, gs.fillSpace = [3]float64{v[0], v[1], v[2]}, 3
			}
		case "k":
			if v, ok := floats(4); ok {
				gs.fill, gs.fillSpace = cmyk(v), 4
			}
		case "cs":
			gs.fillSpace = in.components(res, ops)
			gs.fill = [3]float64{}
		case "sc", "scn":
			gs.fill = colorFrom(ops, gs.fillSpace)
		case "gs":
			if len(ops) > 0 {
				if name, ok := ops[len(ops)-1].(Name); ok {
					gs.opaque = in.opaque(res, name, gs.opaque)
				}
			}
		case "re":
			if v, ok := floats(4); ok {
				path = append(path, boxOf(gs.ctm, v[0], v[1], v[0]+v[2], v[1]+v[3]))
			}
		case "m", "l":
			if v, ok := floats(2); ok {
				x, y := gs.ctm.apply(v[0], v[1])
				pathPts = append(pathPts, [2]float64{x, y})
			}
		case "c", "v", "y":
			pathRect = false
			if v, ok := floats(2); ok {
				x, y := gs.ctm.apply(v[0], v[1])
				pathPts = append(pathPts, [2]float64{x, y})
			}
		case "h":
		case "W", "W*":
			clipping = true
		case "f", "F", "f*", "B", "B*", "b", "b*":
			in.seq++
			if in.hidden == 0 {
				in.canvas.Grounds = append(in.canvas.Grounds, Fill{Box: pathBounds(path, pathPts).intersect(gs.clip), Seq: in.seq, Color: gs.fill, Opaque: gs.opaque})
			}
			if !gs.textClip && in.hidden == 0 {
				for _, r := range append(path, polygonRect(pathPts, pathRect)...) {
					in.canvas.Fills = append(in.canvas.Fills, Fill{Box: r.intersect(gs.clip), Seq: in.seq, Color: gs.fill, Opaque: gs.opaque})
				}
			}
			endPath()
		case "n", "S", "s":
			endPath()
		case "sh":
			in.seq++
			in.canvas.Images = append(in.canvas.Images, Fill{Box: gs.clip, Seq: in.seq})
		case "BT":
			tm, tlm = identity, identity
		case "ET":
			if gs.render >= 4 {
				gs.textClip = true
			}
		case "Tf":
			if len(ops) >= 2 {
				if v, ok := num(ops[len(ops)-1]); ok {
					gs.size = v
				}
				if name, ok := ops[len(ops)-2].(Name); ok {
					gs.font = in.font(res, name)
				}
			}
		case "Tc":
			if v, ok := floats(1); ok {
				gs.tc = v[0]
			}
		case "Tw":
			if v, ok := floats(1); ok {
				gs.tw = v[0]
			}
		case "Tz":
			if v, ok := floats(1); ok {
				gs.th = v[0] / 100
			}
		case "TL":
			if v, ok := floats(1); ok {
				gs.tl = v[0]
			}
		case "Ts":
			if v, ok := floats(1); ok {
				gs.rise = v[0]
			}
		case "Tr":
			if v, ok := floats(1); ok {
				gs.render = int(v[0])
			}
		case "Td":
			if v, ok := floats(2); ok {
				tlm = matrix{1, 0, 0, 1, v[0], v[1]}.mul(tlm)
				tm = tlm
			}
		case "TD":
			if v, ok := floats(2); ok {
				gs.tl = -v[1]
				tlm = matrix{1, 0, 0, 1, v[0], v[1]}.mul(tlm)
				tm = tlm
			}
		case "Tm":
			if v, ok := floats(6); ok {
				tlm = matrix(v)
				tm = tlm
			}
		case "T*":
			tlm = matrix{1, 0, 0, 1, 0, -gs.tl}.mul(tlm)
			tm = tlm
		case "Tj", "'", "\"":
			if op != "Tj" {
				tlm = matrix{1, 0, 0, 1, 0, -gs.tl}.mul(tlm)
				tm = tlm
			}
			if op == "\"" {
				if v, ok := floats(3); ok {
					gs.tw, gs.tc = v[0], v[1]
				}
			}
			if len(ops) > 0 {
				if s, ok := ops[len(ops)-1].(String); ok {
					in.show(s, &tm, gs)
				}
			}
		case "TJ":
			if len(ops) > 0 {
				if arr, ok := ops[len(ops)-1].(Array); ok {
					for _, x := range arr {
						switch v := x.(type) {
						case String:
							in.show(v, &tm, gs)
						case int, float64:
							n, _ := num(v)
							tm = matrix{1, 0, 0, 1, -n / 1000 * gs.size * gs.th, 0}.mul(tm)
						}
					}
				}
			}
		case "Do":
			if len(ops) > 0 {
				if name, ok := ops[len(ops)-1].(Name); ok {
					in.xobject(res, name, gs)
				}
			}
		case "BI":
			skipInlineImage(l)
			in.seq++
			in.canvas.Images = append(in.canvas.Images, Fill{Box: boxOf(gs.ctm, 0, 0, 1, 1).intersect(gs.clip), Seq: in.seq})
		case "BMC":
			in.layers = append(in.layers, false)
		case "BDC":
			off := false
			if len(ops) >= 2 && ops[len(ops)-2] == Name("OC") {
				if name, ok := ops[len(ops)-1].(Name); ok {
					off = in.doc.layerOff(in.doc.Dict(res["Properties"])[name])
				}
			}
			in.layers = append(in.layers, off)
			if off {
				in.hidden++
			}
		case "EMC":
			if n := len(in.layers); n > 0 {
				if in.layers[n-1] {
					in.hidden--
				}
				in.layers = in.layers[:n-1]
			}
		}
		ops = ops[:0]
	}
}

func (in *interp) show(s String, tm *matrix, gs gstate) {
	f := gs.font
	if f == nil {
		f = in.doc.loadFont(nil)
	}
	for _, g := range f.glyphs(s) {
		trm := matrix{gs.size * gs.th, 0, 0, gs.size, 0, gs.rise}.mul(*tm).mul(gs.ctm)
		// Type 3 fonts scale glyph space with their own matrix, so the
		// em square is not the font size given to Tf.
		em := math.Abs(f.scale) * 1000
		box := boxOf(trm, 0, f.descent*em, math.Max(g.width, 0.05), f.ascent*em)
		_, y := trm.apply(0, 0)
		in.seq++
		size := math.Hypot(trm[2], trm[3]) * em
		in.canvas.Glyphs = append(in.canvas.Glyphs, Glyph{
			Text: g.text, Box: box, Size: size, Seq: in.seq, Space: g.space || g.text == " ",
			Invisible: gs.render == 3,
			NewLine:   math.Abs(y-in.lastY) > size/2,
			Color:     gs.fill,
			Stroked:   gs.render == 1 || gs.render == 2 || gs.render == 5 || gs.render == 6,
			Clipped:   box.intersect(gs.clip).area() <= 0,
			Layer:     in.hidden > 0,
		})
		in.lastY = y
		tx := (g.width*gs.size + gs.tc) * gs.th
		if g.space {
			tx += gs.tw * gs.th
		}
		*tm = matrix{1, 0, 0, 1, tx, 0}.mul(*tm)
	}
}

func (in *interp) font(res Dict, name Name) *font {
	ref := in.doc.Dict(res["Font"])[name]
	r, isRef := ref.(Ref)
	if f, ok := in.fonts[r]; ok && isRef {
		return f
	}
	f := in.doc.loadFont(ref)
	if isRef {
		in.fonts[r] = f
	}
	return f
}

func (in *interp) xobject(res Dict, name Name, gs gstate) {
	d := in.doc
	ref := d.Dict(res["XObject"])[name]
	s, ok := d.Resolve(ref).(*Stream)
	if !ok {
		return
	}
	if s.Dict.Name("Subtype") == "Image" {
		in.seq++
		in.canvas.Images = append(in.canvas.Images, Fill{Box: boxOf(gs.ctm, 0, 0, 1, 1).intersect(gs.clip), Seq: in.seq})
		return
	}
	if s.Dict.Name("Subtype") != "Form" {
		return
	}
	if d.layerOff(s.Dict["OC"]) {
		in.hidden++
		defer func() { in.hidden-- }()
	}
	if m, ok := d.Resolve(s.Dict["Matrix"]).(Array); ok && len(m) == 6 {
		var v matrix
		for i := range v {
			v[i], _ = d.Num(m[i])
		}
		gs.ctm = v.mul(gs.ctm)
	}
	if bb, ok := d.rect(s.Dict["BBox"]); ok {
		gs.clip = gs.clip.intersect(boxOf(gs.ctm, bb[0], bb[1], bb[2], bb[3]))
	}
	sub := d.Dict(s.Dict["Resources"])
	if sub == nil {
		sub = res
	}
	in.run(d.Data(s), sub, gs)
}

// opaque applies an ExtGState: a fill that is translucent, soft-masked or
// blended (highlighters use Multiply) does not hide what is beneath it.
func (in *interp) opaque(res Dict, name Name, cur bool) bool {
	g := in.doc.Dict(in.doc.Dict(res["ExtGState"])[name])
	if g == nil {
		return cur
	}
	out := cur
	if v, ok := in.doc.Num(g["ca"]); ok {
		out = v >= 0.95
	}
	if bm, ok := in.doc.Resolve(g["BM"]).(Name); ok && bm != "Normal" && bm != "Compatible" {
		out = false
	}
	if sm, ok := in.doc.Resolve(g["SMask"]).(Dict); ok && sm != nil {
		out = false
	}
	return out
}

func (in *interp) components(res Dict, ops []Object) int {
	if len(ops) == 0 {
		return 1
	}
	name, _ := ops[len(ops)-1].(Name)
	switch name {
	case "DeviceGray", "G", "CalGray":
		return 1
	case "DeviceRGB", "RGB", "CalRGB", "Lab":
		return 3
	case "DeviceCMYK", "CMYK":
		return 4
	case "Pattern":
		return 0
	}
	cs := in.doc.Resolve(in.doc.Dict(res["ColorSpace"])[name])
	if a, ok := cs.(Array); ok && len(a) > 1 {
		switch in.doc.Resolve(a[0]) {
		case Name("ICCBased"):
			if n, ok := in.doc.Num(in.doc.Dict(a[1])["N"]); ok {
				return int(n)
			}
		case Name("Separation"):
			return -1
		case Name("Pattern"):
			return 0
		}
	}
	return 1
}

// colorFrom reads sc/scn operands. A Separation tint of 1 is full ink,
// usually black; patterns have no single colour and count as mid-grey.
func colorFrom(ops []Object, space int) [3]float64 {
	var v []float64
	for _, o := range ops {
		if f, ok := num(o); ok {
			v = append(v, f)
		}
	}
	switch {
	case space == -1 && len(v) >= 1:
		return [3]float64{1 - v[0], 1 - v[0], 1 - v[0]}
	case len(v) >= 4:
		return cmyk(v[len(v)-4:])
	case len(v) >= 3:
		return [3]float64{v[len(v)-3], v[len(v)-2], v[len(v)-1]}
	case len(v) >= 1:
		return [3]float64{v[0], v[0], v[0]}
	}
	return [3]float64{0.5, 0.5, 0.5}
}

func cmyk(v []float64) [3]float64 {
	k := v[3]
	return [3]float64{(1 - v[0]) * (1 - k), (1 - v[1]) * (1 - k), (1 - v[2]) * (1 - k)}
}

func pathBounds(rects []Box, pts [][2]float64) Box {
	b := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, r := range rects {
		b.X0, b.Y0 = math.Min(b.X0, r.X0), math.Min(b.Y0, r.Y0)
		b.X1, b.Y1 = math.Max(b.X1, r.X1), math.Max(b.Y1, r.Y1)
	}
	for _, p := range pts {
		b.X0, b.Y0 = math.Min(b.X0, p[0]), math.Min(b.Y0, p[1])
		b.X1, b.Y1 = math.Max(b.X1, p[0]), math.Max(b.Y1, p[1])
	}
	return b
}

// polygonRect turns a path made of straight segments whose points all sit
// on the corners of their bounding box into that box. Redaction tools draw
// boxes both with "re" and as four-point polygons.
func polygonRect(pts [][2]float64, straight bool) []Box {
	if !straight || len(pts) < 4 || len(pts) > 5 {
		return nil
	}
	b := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		b.X0, b.Y0 = math.Min(b.X0, p[0]), math.Min(b.Y0, p[1])
		b.X1, b.Y1 = math.Max(b.X1, p[0]), math.Max(b.Y1, p[1])
	}
	const eps = 0.5
	near := func(a, c float64) bool { return math.Abs(a-c) < eps }
	for _, p := range pts {
		if !(near(p[0], b.X0) || near(p[0], b.X1)) || !(near(p[1], b.Y0) || near(p[1], b.Y1)) {
			return nil
		}
	}
	return []Box{b}
}

// skipInlineImage moves past BI ... ID <binary> EI. The binary data may
// contain anything, so the end is the first "EI" that stands alone.
func skipInlineImage(l *lexer) {
	i := bytes.Index(l.b[l.pos:], []byte("ID"))
	if i < 0 {
		l.pos = len(l.b)
		return
	}
	p := l.pos + i + 3
	for {
		j := bytes.Index(l.b[p:], []byte("EI"))
		if j < 0 {
			l.pos = len(l.b)
			return
		}
		at := p + j
		before := at == 0 || isSpace(l.b[at-1])
		after := at+2 >= len(l.b) || isSpace(l.b[at+2]) || isDelim(l.b[at+2])
		if before && after {
			l.pos = at + 2
			return
		}
		p = at + 2
	}
}

// annotationFills returns what an annotation paints. Its appearance stream
// is what viewers draw, and it often differs from the interior colour the
// dictionary declares; the declared colour is only used when there is no
// appearance.
func (d *Document) annotationFills(an Dict, rect Box, p Page) []Fill {
	ap := d.Resolve(d.Dict(an["AP"])["N"])
	s, ok := ap.(*Stream)
	if !ok {
		if col, ok := d.color(an["IC"]); ok {
			return []Fill{{Box: rect, Seq: 1 << 30, Color: col, Opaque: true}}
		}
		return nil
	}
	form := identity
	if m, ok := d.Resolve(s.Dict["Matrix"]).(Array); ok && len(m) == 6 {
		for i := range form {
			form[i], _ = d.Num(m[i])
		}
	}
	bb, ok := d.rect(s.Dict["BBox"])
	if !ok {
		return nil
	}
	t := boxOf(form, bb[0], bb[1], bb[2], bb[3])
	if t.X1-t.X0 <= 0 || t.Y1-t.Y0 <= 0 {
		return nil
	}
	sx, sy := (rect.X1-rect.X0)/(t.X1-t.X0), (rect.Y1-rect.Y0)/(t.Y1-t.Y0)
	fit := matrix{sx, 0, 0, sy, rect.X0 - t.X0*sx, rect.Y0 - t.Y0*sy}
	c := &Canvas{}
	in := &interp{doc: d, canvas: c, fonts: map[Ref]*font{}}
	res := d.Dict(s.Dict["Resources"])
	gs := gstate{ctm: form.mul(fit), opaque: true, th: 1, fillSpace: 1, clip: rect}
	in.run(d.Data(s), res, gs)
	for i := range c.Fills {
		c.Fills[i].Seq = 1 << 30
	}
	return c.Fills
}

// layerOff reports whether an optional-content group, or a membership
// dictionary over several groups, is switched off in the default view.
// A membership dictionary is treated as visible when any of its groups
// is on, which is the default AnyOn policy.
func (d *Document) layerOff(o Object) bool {
	if d.offLayers == nil {
		d.offLayers = map[Ref]bool{}
		props := d.Dict(d.Root()["OCProperties"])
		for _, x := range d.list(d.Dict(props["D"])["OFF"]) {
			if r, ok := x.(Ref); ok {
				d.offLayers[r] = true
			}
		}
	}
	if r, ok := o.(Ref); ok && d.offLayers[r] {
		return true
	}
	dict := d.Dict(o)
	if dict == nil || dict.Name("Type") != "OCMD" {
		return false
	}
	groups := d.list(dict["OCGs"])
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if r, ok := g.(Ref); !ok || !d.offLayers[r] {
			return false
		}
	}
	return true
}
