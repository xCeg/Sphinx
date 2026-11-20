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
		if t := strings.TrimSpace(cur.String()); box >= 0 && t != "" {
			out = append(out, Covered{Page: page, Text: t, Dark: luminance(boxes[box].Color) < 0.25})
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

func textOrPlaceholder(g Glyph) string {
	if g.Text == "" {
		return "□"
	}
	return g.Text
}

// hiddenBy returns the index of the first box that hides g, or -1.
func hiddenBy(g Glyph, boxes []Fill) int {
	if g.Space || strings.TrimSpace(g.Text) == "" && g.Text != "" {
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
func PageText(c *Canvas) string {
	var sb strings.Builder
	for _, g := range c.Glyphs {
		if g.NewLine {
			sb.WriteByte('\n')
		}
		sb.WriteString(g.Text)
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
