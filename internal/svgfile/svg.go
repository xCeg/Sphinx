// Package svgfile inspects and seals SVG drawings, which carry what the
// editor knew: the original file name and export path, hidden layers,
// generator comments and the metadata of embedded pictures.
package svgfile

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xceg/sphinx/internal/aitext"
	"github.com/xceg/sphinx/internal/jpegfile"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/pngfile"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/xmp"
)

type passage struct{ reason, text string }

type report struct {
	hidden   []passage
	paths    []string
	trail    []string
	notes    []string
	people   []string
	nested   []leak.Finding
	metadata []byte
}

type element struct {
	name   string
	hidden string
	tiny   bool
}

var (
	styleDecl  = regexp.MustCompile(`(?i)(display\s*:\s*none|visibility\s*:\s*hidden|(?:^|[;\s])opacity\s*:\s*0(?:\.0+)?\s*(?:;|$))`)
	fontSize   = regexp.MustCompile(`(?i)font-size\s*:\s*([0-9.]+)(px|pt)?`)
	pathLike   = regexp.MustCompile(`(?i)(?:file:///?[^\s"<>]+|\b[a-z]:\\[^"<>]*|/(?:Users|home)/[^\s"<>/]+[^"<>]*)`)
	dataURI    = regexp.MustCompile(`data:image/(?:png|jpe?g);base64,([A-Za-z0-9+/=\s]+)`)
	generators = regexp.MustCompile(`(?i)generator|created with|illustrator|inkscape|sketch|figma|affinity|coreldraw`)
)

func hiddenBy(se xml.StartElement) string {
	var style string
	for _, a := range se.Attr {
		v := strings.TrimSpace(a.Value)
		switch a.Name.Local {
		case "style":
			style = v
		case "display":
			if v == "none" {
				return "hidden by its style"
			}
		case "visibility":
			if v == "hidden" {
				return "hidden by its style"
			}
		case "opacity":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f == 0 {
				return "fully transparent"
			}
		}
	}
	if m := styleDecl.FindString(style); m != "" {
		if strings.Contains(strings.ToLower(m), "opacity") {
			return "fully transparent"
		}
		return "hidden by its style"
	}
	return ""
}

func tinyFont(se xml.StartElement) bool {
	for _, a := range se.Attr {
		v := a.Value
		if a.Name.Local == "style" {
			if m := fontSize.FindStringSubmatch(v); m != nil {
				v = m[1]
			} else {
				continue
			}
		} else if a.Name.Local != "font-size" {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimRight(strings.TrimSpace(v), "pxt"), 64)
		if err == nil && f > 0 && f < 1 {
			return true
		}
	}
	return false
}

func analyze(b []byte) *report {
	r := &report{}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	var stack []element
	var text strings.Builder
	hiddenNow := func() (string, bool) {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].hidden != "" {
				return stack[i].hidden, true
			}
		}
		for _, e := range stack {
			if e.tiny {
				return "in a microscopic font", true
			}
		}
		return "", false
	}
	flush := func() {
		if t := strings.Join(strings.Fields(text.String()), " "); t != "" {
			if reason, ok := hiddenNow(); ok {
				r.hidden = append(r.hidden, passage{reason, t})
			}
		}
		text.Reset()
	}
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			flush()
			e := element{name: t.Name.Local, hidden: hiddenBy(t), tiny: tinyFont(t)}
			layer := false
			for _, a := range t.Attr {
				switch {
				case a.Name.Local == "groupmode" && a.Value == "layer":
					layer = true
				case a.Name.Local == "docname":
					r.trail = append(r.trail, "original file name: "+a.Value)
				case a.Name.Local == "export-filename" || a.Name.Local == "absref":
					if pathLike.MatchString(a.Value) {
						r.paths = append(r.paths, a.Value)
					} else {
						r.trail = append(r.trail, "exported as "+a.Value)
					}
				case a.Name.Local == "version" && strings.Contains(a.Name.Space, "inkscape"):
					r.trail = append(r.trail, "Inkscape "+a.Value)
				case a.Name.Local == "href":
					r.paths = append(r.paths, pathLike.FindAllString(a.Value, -1)...)
				}
			}
			if layer && e.hidden != "" {
				e.hidden = "in a hidden layer"
			}
			if t.Name.Local == "metadata" {
				start := d.InputOffset()
				d.Skip()
				r.metadata = b[start:d.InputOffset()]
				continue
			}
			stack = append(stack, e)
		case xml.EndElement:
			flush()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 && (stack[len(stack)-1].name == "text" || stack[len(stack)-1].name == "tspan" || stack[len(stack)-1].name == "textPath") {
				text.Write(t)
				text.WriteByte(' ')
			}
		case xml.Comment:
			c := strings.TrimSpace(string(t))
			switch {
			case c == "":
			case generators.MatchString(c):
				r.trail = append(r.trail, c)
			default:
				r.notes = append(r.notes, c)
			}
		}
	}
	r.readMetadata()
	r.embedded(b)
	return r
}

func (r *report) readMetadata() {
	if len(r.metadata) == 0 {
		return
	}
	x := xmp.Parse(r.metadata)
	if len(x) == 0 {
		// Inkscape writes a bare rdf:RDF block that xmp.Parse also reads;
		// anything else is ignored.
		return
	}
	r.people = append(r.people, x["dc:creator"]...)
	r.people = append(r.people, x["dc:publisher"]...)
	r.trail = append(r.trail, x["xmp:CreatorTool"]...)
	r.paths = append(r.paths, x["stRef:filePath"]...)
}

// embedded inspects pictures stored inline as data: URIs.
func (r *report) embedded(b []byte) {
	for i, m := range dataURI.FindAllSubmatch(b, 64) {
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(m[1])), ""))
		if err != nil {
			continue
		}
		var fs []leak.Finding
		switch sniff.Detect(raw) {
		case sniff.JPEG:
			fs, _ = jpegfile.Inspect(raw)
		case sniff.PNG:
			fs, _ = pngfile.Inspect(raw)
		}
		for _, f := range fs {
			if f.Severity >= leak.Medium {
				f.Message = "A picture inside the drawing: " + strings.ToLower(f.Message[:1]) + f.Message[1:]
				r.nested = append(r.nested, f.At(fmt.Sprintf("embedded image %d", i+1)))
			}
		}
	}
}

func Inspect(b []byte) ([]leak.Finding, error) {
	r := analyze(b)
	var out []leak.Finding
	if len(r.hidden) > 0 {
		var all strings.Builder
		var reasons, quoted []string
		for _, p := range r.hidden {
			all.WriteString(p.text + "\n")
			reasons = append(reasons, p.reason)
			if len(quoted) < 4 {
				quoted = append(quoted, strconv.Quote(photo.Truncate(p.text, 80)))
			}
		}
		if hit := aitext.Match(all.String()); hit != "" {
			out = append(out, leak.HiddenInstructions.Find("Text hidden from human readers gives instructions to the AI tools that will read this drawing.", strconv.Quote(photo.Truncate(hit, 160))))
		}
		out = append(out, leak.InvisibleText.Find(
			fmt.Sprintf("%d piece%s of text cannot be seen (%s).", len(r.hidden), plural(len(r.hidden)), strings.Join(uniq(reasons), ", ")),
			strings.Join(quoted, ", ")))
	}
	if p := uniq(r.paths); len(p) > 0 {
		out = append(out, leak.LocalPaths.Find("The drawing keeps file paths from the computer it was made on.", strings.Join(p, "; ")))
	}
	if p := uniq(r.people); len(p) > 0 {
		out = append(out, leak.PersonalNames.Find("The drawing names its author.", strings.Join(p, "; ")))
	}
	out = append(out, r.nested...)
	if n := uniq(r.notes); len(n) > 0 {
		out = append(out, leak.EmbeddedText.Find("The drawing carries comments.", photo.Truncate(strings.Join(n, " | "), 200)))
	}
	if t := uniq(r.trail); len(t) > 0 {
		out = append(out, leak.SoftwareTrail.Find("The drawing names the software and file it came from.", photo.Truncate(strings.Join(t, "; "), 240)))
	}
	return out, nil
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	r := analyze(b)
	if len(r.hidden) == 0 {
		return nil, nil
	}
	var sb strings.Builder
	for _, p := range r.hidden {
		fmt.Fprintf(&sb, "%s: %s\n", p.reason, p.text)
	}
	return []leak.Artifact{{Name: "invisible-text.txt", Note: "text drawn so that readers cannot see it", Data: []byte(sb.String())}}, nil
}

var (
	comments      = regexp.MustCompile(`<!--[\s\S]*?-->`)
	metadataBlock = regexp.MustCompile(`(?s)<metadata\b[^>]*/>|<metadata\b.*?</metadata>`)
	namedview     = regexp.MustCompile(`(?s)<sodipodi:namedview\b[^>]*/>|<sodipodi:namedview\b.*?</sodipodi:namedview>`)
	editorAttrs   = regexp.MustCompile(`\s(?:sodipodi|inkscape):(?:docname|export-filename|export-xdpi|export-ydpi|version|absref)="[^"]*"`)
)

// Seal removes comments, metadata, the editor's view settings and its
// file names, and seals embedded pictures. Hidden layers and hidden text
// are content and stay, as does the post-seal report about them.
func Seal(b []byte) ([]byte, error) {
	out := comments.ReplaceAll(b, nil)
	out = metadataBlock.ReplaceAll(out, nil)
	out = namedview.ReplaceAll(out, nil)
	out = editorAttrs.ReplaceAll(out, nil)
	out = dataURI.ReplaceAllFunc(out, func(m []byte) []byte {
		sub := dataURI.FindSubmatch(m)
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(sub[1])), ""))
		if err != nil {
			return m
		}
		var sealed []byte
		switch sniff.Detect(raw) {
		case sniff.JPEG:
			sealed, err = jpegfile.Seal(raw)
		case sniff.PNG:
			sealed, err = pngfile.Seal(raw)
		default:
			return m
		}
		if err != nil {
			return m
		}
		head := m[:bytes.Index(m, []byte(","))+1]
		return append(append([]byte(nil), head...), base64.StdEncoding.EncodeToString(sealed)...)
	})
	return out, nil
}

func uniq(vs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
