package pdf

import (
	"regexp"
	"strings"
	"unicode"
)

// Invisible is text that is present but drawn so that no reader sees it.
type Invisible struct {
	Page   int
	Text   string
	Reason string
}

// Glyphs below this size cannot be read at any normal zoom; text set this
// small is there for machines. The smallest legible print, footnote and
// legal small print, stays above 4 pt.
const microscopic = 1.0

// Text that runs past the edge of the page or of its clipping area is
// usually an overflowing line, cut mid-word. Reporting it takes a run long
// enough to say something; in the calibration corpus shorter runs were all
// fragments such as "nks" or "side up.".
const minEdgeLetters = 15

// generatorCredit matches the credit lines some PDF libraries print in
// microscopic or invisible type. They name the software, nothing more.
var generatorCredit = regexp.MustCompile(`(?i)^(powered|created|generated|produced|made)\s+(by|with)\b`)

// invisibleReason explains why a glyph cannot be seen, or returns "".
func invisibleReason(g Glyph, c *Canvas) string {
	cx, cy := (g.Box.X0+g.Box.X1)/2, (g.Box.Y0+g.Box.Y1)/2
	switch {
	case g.Layer:
		return "in a layer that is switched off"
	case g.Size > 0 && g.Size < microscopic:
		return "in a microscopic font"
	case g.Box.intersect(c.Page).area() <= 0:
		return "outside the page"
	case g.Clipped:
		return "clipped out of view"
	case g.Invisible && !overImage(cx, cy, g.Seq, c):
		return "in invisible ink"
	case !g.Invisible && !g.Stroked && luminance(g.Color) > 0.95 && !onDarkGround(cx, cy, g.Seq, c):
		return "white on white"
	}
	return ""
}

// overImage tells an OCR layer, invisible text laid over a scanned page so
// it can be searched, from invisible text with nothing under it.
func overImage(x, y float64, seq int, c *Canvas) bool {
	for _, im := range c.Images {
		if im.Seq < seq && im.Box.contains(x, y) {
			return true
		}
	}
	return false
}

// onDarkGround accepts white text printed over a coloured box or a picture.
func onDarkGround(x, y float64, seq int, c *Canvas) bool {
	if overImage(x, y, seq, c) {
		return true
	}
	var under *Fill
	for i := range c.Grounds {
		f := &c.Grounds[i]
		if f.Seq < seq && f.Box.contains(x, y) && (under == nil || f.Seq > under.Seq) {
			under = f
		}
	}
	if under != nil && luminance(under.Color) < 0.9 {
		return true
	}
	// Display type often draws a dark copy of a word, then the white word
	// on top of it, as a shadow or outline.
	for _, g := range c.Glyphs {
		if g.Seq >= seq {
			break
		}
		if !g.Invisible && luminance(g.Color) < 0.6 && g.Box.contains(x, y) {
			return true
		}
	}
	return false
}

// invisibleText groups consecutive invisible glyphs that share a reason.
func invisibleText(page int, c *Canvas) []Invisible {
	var out []Invisible
	var cur strings.Builder
	reason := ""
	var prev *Glyph
	flush := func() {
		t := strings.TrimSpace(cur.String())
		if reason != "" && meaningful(t, false) && !generatorCredit.MatchString(strings.Join(strings.Fields(t), " ")) && (!atEdge(reason) || letters(t) >= minEdgeLetters) {
			out = append(out, Invisible{page, t, reason})
		}
		cur.Reset()
		reason = ""
		prev = nil
	}
	for i := range c.Glyphs {
		g := &c.Glyphs[i]
		r := ""
		if !g.Space {
			r = invisibleReason(*g, c)
		}
		switch {
		case r != "":
			if r != reason {
				flush()
				reason = r
			} else if g.NewLine || (prev != nil && g.Box.X0-prev.Box.X1 > g.Size/4) {
				cur.WriteByte(' ')
			}
			cur.WriteString(textOrPlaceholder(*g))
			prev = g
		case g.Space && reason != "":
			cur.WriteByte(' ')
		default:
			flush()
		}
	}
	flush()
	return out
}

func atEdge(reason string) bool {
	return reason == "outside the page" || reason == "clipped out of view"
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}
