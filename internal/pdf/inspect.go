package pdf

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/xmp"
)

// Analysis is everything Sphinx learned from one PDF.
type Analysis struct {
	Covered     []Covered
	Redactions  []Covered
	Comments    []string
	Revisions   []Revision
	Attachments []Attachment
	Findings    []leak.Finding
}

type Revision struct {
	Number  int
	Bytes   []byte
	Removed []string
}

type Attachment struct {
	Name string
	Data []byte
}

func Analyze(b []byte) (*Analysis, error) {
	f, err := Parse(b)
	if err != nil {
		return nil, err
	}
	d := f.Final()
	if d.Encrypted() {
		return nil, ErrEncrypted
	}
	a := &Analysis{}
	final := a.pages(d)
	a.revisions(f, final)
	a.attachments(d)
	a.findings(d)
	return a, nil
}

func (a *Analysis) pages(d *Document) map[string]bool {
	text := map[string]bool{}
	for i, p := range d.Pages() {
		n := i + 1
		c := d.Render(p)
		for w := range words(PageText(c)) {
			text[w] = true
		}
		var overlays []Fill
		for _, ao := range d.list(p.Dict["Annots"]) {
			an := d.Dict(ao)
			if an == nil {
				continue
			}
			r, ok := d.rect(an["Rect"])
			if !ok {
				continue
			}
			rect := Box{r[0], r[1], r[2], r[3]}
			switch an.Name("Subtype") {
			case "Redact":
				a.Redactions = append(a.Redactions, coveredText(n, c.Glyphs, []Fill{{Box: rect, Seq: 1 << 30, Opaque: true}})...)
			case "Square", "Polygon":
				if flags, _ := d.Num(an["F"]); int(flags)&(2|32) == 0 && d.opacity(an) >= 0.95 {
					overlays = append(overlays, d.annotationFills(an, rect, p)...)
				}
			}
			if txt := d.text(an["Contents"]); txt != "" && commentTypes[an.Name("Subtype")] {
				who := d.text(an["T"])
				if who != "" {
					txt = who + ": " + txt
				}
				a.Comments = append(a.Comments, fmt.Sprintf("page %d, %s", n, txt))
			}
		}
		a.Covered = append(a.Covered, coveredText(n, c.Glyphs, append(c.Fills, overlays...))...)
	}
	return text
}

var commentTypes = map[Name]bool{
	"Text": true, "FreeText": true, "Highlight": true, "Underline": true, "StrikeOut": true,
	"Squiggly": true, "Caret": true, "Square": true, "Circle": true, "Ink": true, "Polygon": true, "Line": true,
}

func (a *Analysis) revisions(f *File, final map[string]bool) {
	revs := f.Revisions()
	for i, end := range revs[:len(revs)-1] {
		d := f.At(end)
		old := map[string]bool{}
		for _, p := range d.Pages() {
			for w := range words(PageText(d.Render(p))) {
				old[w] = true
			}
		}
		var removed []string
		for w := range old {
			if !final[w] {
				removed = append(removed, w)
			}
		}
		sort.Strings(removed)
		a.Revisions = append(a.Revisions, Revision{Number: i + 1, Bytes: d.Bytes(), Removed: removed})
	}
}

func (a *Analysis) attachments(d *Document) {
	root := d.Root()
	if root == nil {
		return
	}
	var walk func(o Object, depth int)
	walk = func(o Object, depth int) {
		node := d.Dict(o)
		if node == nil || depth > 32 {
			return
		}
		names, _ := d.Resolve(node["Names"]).(Array)
		for i := 0; i+1 < len(names); i += 2 {
			a.addFileSpec(d, d.text(names[i]), names[i+1])
		}
		for _, k := range d.list(node["Kids"]) {
			walk(k, depth+1)
		}
	}
	walk(d.Dict(root["Names"])["EmbeddedFiles"], 0)
	for _, p := range d.Pages() {
		for _, ao := range d.list(p.Dict["Annots"]) {
			if an := d.Dict(ao); an != nil && an.Name("Subtype") == "FileAttachment" {
				a.addFileSpec(d, "", an["FS"])
			}
		}
	}
}

func (a *Analysis) addFileSpec(d *Document, name string, spec Object) {
	fs := d.Dict(spec)
	if fs == nil {
		return
	}
	for _, k := range []Name{"UF", "F"} {
		if t := d.text(fs[k]); t != "" {
			name = t
			break
		}
	}
	ef := d.Dict(fs["EF"])
	data := d.Data(ef["F"])
	if data == nil {
		data = d.Data(ef["UF"])
	}
	if name == "" {
		name = fmt.Sprintf("attachment-%d", len(a.Attachments)+1)
	}
	a.Attachments = append(a.Attachments, Attachment{Name: name, Data: data})
}

func (a *Analysis) findings(d *Document) {
	add := func(f leak.Finding) { a.Findings = append(a.Findings, f) }

	if len(a.Redactions) > 0 {
		add(leak.UnappliedRedaction.Find(
			fmt.Sprintf("%d area%s marked for redaction still show%s the original text underneath, on %s.", len(a.Redactions), plural(len(a.Redactions)), verbS(len(a.Redactions)), pageList(a.Redactions)),
			quoteSamples(a.Redactions)))
	}
	var dark, other []Covered
	for _, c := range a.Covered {
		if c.Dark {
			dark = append(dark, c)
		} else {
			other = append(other, c)
		}
	}
	if n := len(dark); n > 0 {
		add(leak.BlackedOutText.Find(
			fmt.Sprintf("%s hidden under %s can still be copied or extracted, on %s.", count(n, "passage"), pick(n, "a black box", "black boxes"), pageList(dark)),
			quoteSamples(dark)))
	}
	if n := len(other); n > 0 {
		add(leak.BlackedOutText.Find(
			fmt.Sprintf("%s covered by %s painted over %s can still be copied or extracted, on %s.", count(n, "passage"), pick(n, "a shape", "shapes"), pick(n, "it", "them"), pageList(other)),
			quoteSamples(other)).Lower(leak.High))
	}

	for _, r := range a.Revisions {
		if len(r.Removed) > 0 {
			add(leak.EarlierRevision.Find(
				fmt.Sprintf("An earlier version (revision %d of %d) is still inside the file, and it contains %d word%s that the final version no longer shows.", r.Number, len(a.Revisions)+1, len(r.Removed), plural(len(r.Removed))),
				sampleWords(r.Removed)))
		}
	}
	if n := len(a.Revisions); n > 0 && !anyRemoved(a.Revisions) {
		add(leak.EarlierRevision.Find(
			fmt.Sprintf("The file keeps %d earlier version%s of itself. Their text is the same as the final version, but layout, annotations or form values may differ.", n, plural(n)),
			"").Lower(leak.Low))
	}

	if len(a.Comments) > 0 {
		add(leak.Comments.Find(fmt.Sprintf("The document carries %d comment%s.", len(a.Comments), plural(len(a.Comments))), truncate(strings.Join(a.Comments, " | "), 300)))
	}
	if n := len(a.Attachments); n > 0 {
		var names []string
		for _, at := range a.Attachments {
			names = append(names, at.Name)
		}
		add(leak.EmbeddedFiles.Find(fmt.Sprintf("%d file%s %s attached inside the document.", n, plural(n), isAre(n)), strings.Join(names, ", ")))
	}
	a.metadata(d, add)
}

func (a *Analysis) metadata(d *Document, add func(leak.Finding)) {
	info := d.Info()
	x := xmp.Packet{}
	if root := d.Root(); root != nil {
		x = xmp.Parse(d.Data(root["Metadata"]))
	}
	str := func(k Name) string { return d.text(info[k]) }

	people := uniq(append([]string{str("Author")}, x["dc:creator"]...)...)
	for k := range info {
		if k == "Company" || k == "Manager" || k == "LastModifiedBy" {
			if v := str(k); v != "" {
				people = uniq(append(people, string(k)+": "+v)...)
			}
		}
	}
	if len(people) > 0 {
		add(leak.PersonalNames.Find("The document names the people or organisation behind it.", strings.Join(people, "; ")))
	}

	var paths []string
	for _, v := range []string{str("Title"), str("Subject"), str("Keywords"), x.First("dc:title")} {
		paths = append(paths, findPaths(v)...)
	}
	for _, v := range x["stRef:filePath"] {
		paths = append(paths, findPaths(v)...)
	}
	paths = append(paths, a.linkPaths(d)...)
	if paths = uniq(paths...); len(paths) > 0 {
		add(leak.LocalPaths.Find("The document contains file paths from the computer it was made on.", strings.Join(paths, "; ")))
	}

	trail := []string{str("Creator"), str("Producer"), x.First("xmp:CreatorTool"), x.First("pdf:Producer")}
	if t := str("Title"); strings.Contains(t, " - ") && officeTitle.MatchString(t) {
		trail = append(trail, "original file name: "+t)
	}
	if trail = uniq(trail...); len(trail) > 0 {
		add(leak.SoftwareTrail.Find("The document names the software that produced it.", strings.Join(trail, "; ")))
	}

	var when []string
	for _, k := range []Name{"CreationDate", "ModDate"} {
		if v := pdfDate(str(k)); v != "" {
			when = append(when, dateLabel[k]+" "+v)
		}
	}
	if len(when) > 0 {
		add(leak.Timestamps.Find("The document records when it was created and last changed.", strings.Join(when, "; ")))
	}
}

var dateLabel = map[Name]string{"CreationDate": "created", "ModDate": "modified"}

var officeTitle = regexp.MustCompile(`^Microsoft (Word|PowerPoint|Excel) - `)

// linkPaths collects file: links and launch actions that point into the
// author's own disk.
func (a *Analysis) linkPaths(d *Document) []string {
	var out []string
	for _, p := range d.Pages() {
		for _, ao := range d.list(p.Dict["Annots"]) {
			an := d.Dict(ao)
			if an == nil || an.Name("Subtype") != "Link" {
				continue
			}
			act := d.Dict(an["A"])
			for _, v := range []Object{act["URI"], d.Dict(act["F"])["F"], act["F"]} {
				out = append(out, findPaths(d.text(v))...)
			}
		}
	}
	return out
}

var pathLike = regexp.MustCompile(`(?i)(?:file:///?[^\s"<>]+|\b[a-z]:\\[^\s"<>]*|/(?:Users|home)/[^\s"<>/]+[^\s"<>]*|\\\\[a-z0-9_.-]+\\[^\s"<>]*)`)

func findPaths(s string) []string { return pathLike.FindAllString(s, -1) }

// text decodes a PDF text string: UTF-16BE with a byte-order mark, UTF-8
// with one, or PDFDocEncoding, approximated here by Windows-1252.
func (d *Document) text(o Object) string {
	s, ok := d.Resolve(o).(String)
	if !ok {
		return ""
	}
	var out string
	switch {
	case len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF:
		out = utf16Text(s[2:])
	case len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF:
		out = string(s[3:])
	default:
		enc := baseEncoding("WinAnsiEncoding")
		var sb strings.Builder
		for _, c := range s {
			sb.WriteString(enc[c])
		}
		out = sb.String()
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == 0 {
			return -1
		}
		return r
	}, out))
}

func (d *Document) color(o Object) ([3]float64, bool) {
	a, ok := d.Resolve(o).(Array)
	if !ok {
		return [3]float64{}, false
	}
	var v []float64
	for _, x := range a {
		if f, ok := d.Num(x); ok {
			v = append(v, f)
		}
	}
	switch len(v) {
	case 1:
		return [3]float64{v[0], v[0], v[0]}, true
	case 3:
		return [3]float64{v[0], v[1], v[2]}, true
	case 4:
		return cmyk(v), true
	}
	return [3]float64{}, false
}

func (d *Document) opacity(an Dict) float64 {
	if v, ok := d.Num(an["CA"]); ok {
		return v
	}
	return 1
}

var pdfDateRe = regexp.MustCompile(`^(?:D:)?(\d{4})(\d{2})?(\d{2})?(\d{2})?(\d{2})?(\d{2})?([Zz+\-])?(\d{2})?'?(\d{2})?`)

func pdfDate(s string) string {
	m := pdfDateRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	pad := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	out := fmt.Sprintf("%s-%s-%s %s:%s:%s", m[1], pad(m[2], "01"), pad(m[3], "01"), pad(m[4], "00"), pad(m[5], "00"), pad(m[6], "00"))
	switch m[7] {
	case "Z", "z":
		out += " UTC"
	case "+", "-":
		out += " " + m[7] + pad(m[8], "00") + ":" + pad(m[9], "00")
	}
	return out
}

func anyRemoved(rs []Revision) bool {
	for _, r := range rs {
		if len(r.Removed) > 0 {
			return true
		}
	}
	return false
}

func pageList(cs []Covered) string {
	seen := map[int]bool{}
	var pages []int
	for _, c := range cs {
		if !seen[c.Page] {
			seen[c.Page] = true
			pages = append(pages, c.Page)
		}
	}
	sort.Ints(pages)
	var s []string
	for _, p := range pages[:min(len(pages), 8)] {
		s = append(s, strconv.Itoa(p))
	}
	label := "page "
	if len(pages) > 1 {
		label = "pages "
	}
	out := label + strings.Join(s, ", ")
	if len(pages) > 8 {
		out += fmt.Sprintf(" and %d more", len(pages)-8)
	}
	return out
}

func quoteSamples(cs []Covered) string {
	var q []string
	for _, c := range cs[:min(len(cs), 4)] {
		q = append(q, strconv.Quote(truncate(c.Text, 80)))
	}
	if len(cs) > 4 {
		q = append(q, fmt.Sprintf("and %d more", len(cs)-4))
	}
	return strings.Join(q, ", ")
}

func sampleWords(ws []string) string {
	out := strings.Join(ws[:min(len(ws), 12)], ", ")
	if len(ws) > 12 {
		out += fmt.Sprintf(" and %d more", len(ws)-12)
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func uniq(vs ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	return out
}

func count(n int, noun string) string { return fmt.Sprintf("%d %s%s", n, noun, plural(n)) }

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func verbS(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
