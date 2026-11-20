package pdf

import (
	"strings"
	"unicode"
)

// Covered is text that is present in the file but painted over.
type Covered struct {
	Page int
	Text string
	Dark bool
}

func luminance(c [3]float64) float64 { return 0.299*c[0] + 0.587*c[1] + 0.114*c[2] }

// coveredText finds glyphs that an opaque rectangle, painted after them,
// hides. A glyph counts as hidden when its centre is inside the box and
// the box covers most of it, so that underlines and table rules drawn
// after the text do not qualify.
func coveredText(page int, glyphs []Glyph, fills []Fill) []Covered {
	var boxes []Fill
	for _, f := range fills {
		if f.Opaque && f.Box.area() >= 4 {
			boxes = append(boxes, f)
		}
	}
	if len(boxes) == 0 {
		return nil
	}
	var out []Covered
	var cur strings.Builder
	box := -1
	flush := func() {
		t := strings.TrimSpace(cur.String())
		if box >= 0 {
			dark := luminance(boxes[box].Color) < 0.25
			if meaningful(t, dark) && !reprinted(t, boxes[box], glyphs) {
				out = append(out, Covered{Page: page, Text: t, Dark: dark})
			}
		}
		cur.Reset()
		box = -1
	}
	for _, g := range glyphs {
		i := hiddenBy(g, boxes)
		switch {
		case i >= 0:
			if i != box {
				flush()
			} else if g.NewLine {
				cur.WriteByte(' ')
			}
			cur.WriteString(textOrPlaceholder(g))
			box = i
		case g.Space && box >= 0:
			cur.WriteByte(' ')
		default:
			flush()
		}
	}
	flush()
	return out
}

// meaningful rejects runs with no letter or digit: bullets, rules drawn
// with dashes, glyphs whose font has no text mapping. Under a shape that
// is not black, one or two characters are usually a chart label or a
// stamp overlapping a letter, not an attempt to hide anything; the
// calibration corpus had several and no real case.
func meaningful(t string, dark bool) bool {
	n := 0
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n >= 1 && (dark || n >= 3)
}

// reprinted reports whether the covered words are painted again on top
// of the box. Layout tools do this when text is edited: the old copy is
// boxed over and the new one drawn above it. The hidden copy says nothing
// the page does not already show.
func reprinted(t string, box Fill, glyphs []Glyph) bool {
	var above strings.Builder
	for _, g := range glyphs {
		cx, cy := (g.Box.X0+g.Box.X1)/2, (g.Box.Y0+g.Box.Y1)/2
		if g.Seq > box.Seq && !g.Invisible && box.Box.contains(cx, cy) {
			if g.NewLine {
				above.WriteByte(' ')
			}
			above.WriteString(g.Text)
		}
	}
	shown := words(above.String())
	if len(shown) == 0 {
		return false
	}
	for w := range words(t) {
		if !shown[w] && !partOfAny(w, shown) {
			return false
		}
	}
	return true
}

// partOfAny accepts words cut by the edge of the box.
func partOfAny(w string, shown map[string]bool) bool {
	for s := range shown {
		if strings.HasPrefix(s, w) || strings.HasSuffix(s, w) {
			return true
		}
	}
	return false
}

func textOrPlaceholder(g Glyph) string {
	if g.Text == "" {
		return "□"
	}
	return g.Text
}

// hiddenBy returns the index of the first box that hides g, or -1.
func hiddenBy(g Glyph, boxes []Fill) int {
	if g.Space || g.Invisible || strings.TrimSpace(g.Text) == "" && g.Text != "" {
		return -1
	}
	area := g.Box.area()
	if area <= 0 {
		return -1
	}
	cx, cy := (g.Box.X0+g.Box.X1)/2, (g.Box.Y0+g.Box.Y1)/2
	for i, f := range boxes {
		if f.Seq > g.Seq && f.Box.contains(cx, cy) && f.Box.intersect(g.Box).area() >= 0.6*area {
			return i
		}
	}
	return -1
}

// PageText returns the visible reading-order text of a canvas, used to
// compare revisions.
// Many producers draw each word or line as its own string with no space
// glyph between them, so a horizontal gap wider than a quarter of the font
// size also counts as a word break.
func PageText(c *Canvas) string {
	var sb strings.Builder
	var prev *Glyph
	for i := range c.Glyphs {
		g := &c.Glyphs[i]
		switch {
		case g.NewLine:
			sb.WriteByte('\n')
		case prev != nil && g.Box.X0-prev.Box.X1 > g.Size/4:
			sb.WriteByte(' ')
		}
		sb.WriteString(g.Text)
		prev = g
	}
	return sb.String()
}

// words splits text into lower-case words of two or more letters or digits.
func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 2 {
			out[strings.ToLower(w)] = true
		}
	}
	return out
}
