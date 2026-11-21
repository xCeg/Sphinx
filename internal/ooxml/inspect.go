package ooxml

import (
	"encoding/xml"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/xceg/sphinx/internal/aitext"
	"github.com/xceg/sphinx/internal/jpegfile"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/pngfile"
	"github.com/xceg/sphinx/internal/sniff"
)

// passage is a piece of text with who wrote it and where it is.
type passage struct {
	Where, Author, Text string
}

type report struct {
	deleted     []passage
	comments    []passage
	hidden      []passage
	invisible   []passage
	people      []string
	paths       []string
	embedded    []string
	trail       []string
	when        []string
	nested      []leak.Finding
	hiddenParts map[string][]byte
}

func Inspect(b []byte) ([]leak.Finding, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	r := analyze(p)
	return r.findings(), nil
}

func analyze(p *pkg) *report {
	r := &report{hiddenParts: map[string][]byte{}}
	r.properties(p)
	r.word(p)
	r.excel(p)
	r.powerpoint(p)
	r.links(p)
	r.printers(p)
	r.media(p)
	return r
}

func (r *report) findings() []leak.Finding {
	var out []leak.Finding
	if n := len(r.deleted); n > 0 {
		out = append(out, leak.DeletedText.Find(
			fmt.Sprintf("%d deleted passage%s %s still stored as tracked changes%s.", n, plural(n), isAre(n), byAuthors(r.deleted)),
			samples(r.deleted)))
	}
	if n := len(r.hidden); n > 0 {
		out = append(out, leak.HiddenContent.Find(
			fmt.Sprintf("The file holds content that is not visible when you read it: %s.", describeHidden(r.hidden)),
			samples(r.hidden)))
	}
	if n := len(r.invisible); n > 0 {
		var reasons []string
		for _, p := range r.invisible {
			reasons = append(reasons, p.Where)
		}
		out = append(out, leak.InvisibleText.Find(
			fmt.Sprintf("%d passage%s of text cannot be seen (%s).", n, plural(n), strings.Join(uniq(reasons...), ", ")),
			samples(r.invisible)))
	}
	var concealed strings.Builder
	for _, p := range append(append([]passage(nil), r.hidden...), r.invisible...) {
		concealed.WriteString(p.Text + "\n")
	}
	if hit := aitext.Match(concealed.String()); hit != "" {
		out = append([]leak.Finding{leak.HiddenInstructions.Find(
			"Text hidden from human readers gives instructions to the AI tools that will read this document.",
			strconv.Quote(truncate(hit, 160)))}, out...)
	}
	if n := len(r.comments); n > 0 {
		out = append(out, leak.Comments.Find(
			fmt.Sprintf("%d comment%s%s %s stored in the file.", n, plural(n), byAuthors(r.comments), isAre(n)),
			samples(r.comments)))
	}
	if people := uniq(r.people...); len(people) > 0 {
		out = append(out, leak.PersonalNames.Find("The file names the people and organisation who created and edited it.", strings.Join(people, "; ")))
	}
	if paths := uniq(r.paths...); len(paths) > 0 {
		out = append(out, leak.LocalPaths.Find("The file contains paths from the computer it was made on.", strings.Join(paths, "; ")))
	}
	if n := len(r.embedded); n > 0 {
		out = append(out, leak.EmbeddedFiles.Find(fmt.Sprintf("%d embedded object%s travel%s with the file.", n, plural(n), verbS(n)), strings.Join(r.embedded, "; ")))
	}
	out = append(out, r.nested...)
	if trail := uniq(r.trail...); len(trail) > 0 {
		out = append(out, leak.SoftwareTrail.Find("The file records the software and editing history behind it.", strings.Join(trail, "; ")))
	}
	if when := uniq(r.when...); len(when) > 0 {
		out = append(out, leak.Timestamps.Find("The file records when it was created, edited or printed.", strings.Join(when, "; ")))
	}
	return out
}

func (r *report) properties(p *pkg) {
	walk(p.data("docProps/core.xml"), nil, func(s string, stack []string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		switch top(stack) {
		case "creator":
			r.people = append(r.people, "author: "+s)
		case "lastModifiedBy":
			r.people = append(r.people, "last saved by: "+s)
		case "created":
			r.when = append(r.when, "created "+s)
		case "modified":
			r.when = append(r.when, "modified "+s)
		case "lastPrinted":
			r.when = append(r.when, "printed "+s)
		case "revision":
			if n, _ := strconv.Atoi(s); n > 1 {
				r.trail = append(r.trail, "saved "+s+" times")
			}
		}
	})
	walk(p.data("docProps/app.xml"), nil, func(s string, stack []string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		switch top(stack) {
		case "Company":
			r.people = append(r.people, "company: "+s)
		case "Manager":
			r.people = append(r.people, "manager: "+s)
		case "Application":
			r.trail = append(r.trail, s)
		case "TotalTime":
			if m, _ := strconv.Atoi(s); m > 0 {
				r.trail = append(r.trail, fmt.Sprintf("edited for %s in total", minutes(m)))
			}
		case "Template":
			r.paths = append(r.paths, findPaths(s)...)
		}
	})
	walk(p.data("docProps/custom.xml"), nil, func(s string, stack []string) {
		r.people = append(r.people, emails.FindAllString(s, -1)...)
	})
}

var emails = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// run is the formatting of the Word run being read.
type run struct {
	vanish, white, tiny, shaded bool
}

func (r run) invisible() string {
	switch {
	case r.white && !r.shaded:
		return "white on white"
	case r.tiny:
		return "in a microscopic font"
	}
	return ""
}

func (r *report) word(p *pkg) {
	parts := []string{"word/document.xml"}
	parts = append(parts, p.glob("word", "header")...)
	parts = append(parts, p.glob("word", "footer")...)
	parts = append(parts, "word/footnotes.xml", "word/endnotes.xml")
	for _, name := range parts {
		var del, hid, inv strings.Builder
		var delAuthor, invReason string
		flush := func() {
			if t := strings.TrimSpace(del.String()); t != "" {
				r.deleted = append(r.deleted, passage{name, delAuthor, t})
			}
			if t := strings.TrimSpace(hid.String()); t != "" {
				r.hidden = append(r.hidden, passage{"hidden text", "", t})
			}
			if t := strings.TrimSpace(inv.String()); t != "" {
				r.invisible = append(r.invisible, passage{invReason, "", t})
			}
			del.Reset()
			hid.Reset()
			inv.Reset()
		}
		var cur run
		paraShaded, cellShaded := false, false
		walk(p.data(name), func(se xml.StartElement, stack []string) {
			inRun := in(stack, "rPr") && !in(stack, "pPr")
			switch se.Name.Local {
			case "del":
				flush()
				delAuthor = attr(se, "author")
			case "r":
				// Text boxes and shapes have their own fill, which is not
				// read here, so white text inside them is given the benefit
				// of the doubt.
				cur = run{shaded: paraShaded || cellShaded || in(stack, "txbxContent")}
			case "tc":
				cellShaded = false
			case "p":
				flush()
				paraShaded = false
			case "vanish":
				if inRun && attr(se, "val") != "0" && attr(se, "val") != "false" {
					cur.vanish = true
				}
			case "color":
				if inRun && strings.EqualFold(attr(se, "val"), "FFFFFF") {
					cur.white = true
				}
			case "sz":
				// Sizes are in half-points; 2 is a one-point font.
				if v, err := strconv.Atoi(attr(se, "val")); inRun && err == nil && v <= 2 {
					cur.tiny = true
				}
			case "shd", "highlight":
				fill := strings.ToLower(attr(se, "fill") + attr(se, "val"))
				dark := fill != "" && fill != "auto" && fill != "ffffff" && fill != "white" && fill != "none" && fill != "clear"
				if dark && in(stack, "pPr") {
					paraShaded = true
				}
				if dark && in(stack, "tcPr") {
					cellShaded = true
				}
				if dark && inRun {
					cur.shaded = true
				}
			}
		}, func(s string, stack []string) {
			switch top(stack) {
			case "delText":
				del.WriteString(s)
			case "t":
				if in(stack, "del") {
					return
				}
				if cur.vanish {
					hid.WriteString(s)
				} else if reason := cur.invisible(); reason != "" {
					if invReason != "" && reason != invReason {
						flush()
					}
					invReason = reason
					inv.WriteString(s)
				}
			}
		})
		flush()
	}
	walk(p.data("word/settings.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "docVar" {
			if v := strings.TrimSpace(attr(se, "val")); v != "" {
				r.hidden = append(r.hidden, passage{"document variables", "", attr(se, "name") + " = " + v})
			}
		}
	}, nil)
	r.commentParts(p, "word/comments.xml", "comment", "author")
	walk(p.data("word/people.xml"), func(se xml.StartElement, _ []string) {
		switch se.Name.Local {
		case "person":
			r.people = append(r.people, attr(se, "author"))
		case "presenceInfo":
			if id := attr(se, "userId"); emails.MatchString(id) {
				r.people = append(r.people, id)
			}
		}
	}, nil)
}

func (r *report) commentParts(p *pkg, name, element, authorAttr string) {
	var cur *passage
	walk(p.data(name), func(se xml.StartElement, _ []string) {
		if se.Name.Local == element {
			r.comments = append(r.comments, passage{Where: name, Author: attr(se, authorAttr)})
			cur = &r.comments[len(r.comments)-1]
		}
	}, func(s string, stack []string) {
		if cur != nil && (top(stack) == "t" || top(stack) == "text") {
			cur.Text += s
		}
	})
}

func (r *report) excel(p *pkg) {
	if p.parts["xl/workbook.xml"] == nil {
		return
	}
	strs := sharedStrings(p)
	sheetFiles := workbookTargets(p)
	walk(p.data("xl/workbook.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local != "sheet" {
			return
		}
		state := attr(se, "state")
		if state != "hidden" && state != "veryHidden" {
			return
		}
		name := attr(se, "name")
		part := sheetFiles[relID(se)]
		cells := sheetCells(p.data(part), strs, nil)
		label := fmt.Sprintf("%s sheet %q", state, name)
		if state == "veryHidden" {
			label = fmt.Sprintf("very hidden sheet %q (cannot be unhidden from the Excel menus)", name)
		}
		r.hidden = append(r.hidden, passage{label, "", strings.Join(cells, " ")})
		r.hiddenParts["hidden-sheet-"+safe(name)+".txt"] = []byte(strings.Join(cells, "\n") + "\n")
	}, nil)
	for _, part := range p.glob("xl/worksheets", "sheet") {
		if cells, n := hiddenColumns(p.data(part), strs); len(cells) > 0 {
			r.hidden = append(r.hidden, passage{fmt.Sprintf("%d hidden column%s in %s", n, plural(n), path.Base(part)), "", strings.Join(cells, " ")})
		}
		hiddenRows := map[string]bool{}
		walk(p.data(part), func(se xml.StartElement, _ []string) {
			if se.Name.Local == "row" && (attr(se, "hidden") == "1" || attr(se, "hidden") == "true") {
				hiddenRows[attr(se, "r")] = true
			}
		}, nil)
		if len(hiddenRows) == 0 {
			continue
		}
		cells := sheetCells(p.data(part), strs, hiddenRows)
		if len(cells) > 0 {
			r.hidden = append(r.hidden, passage{fmt.Sprintf("%d hidden row%s in %s", len(hiddenRows), plural(len(hiddenRows)), path.Base(part)), "", strings.Join(cells, " ")})
		}
	}
	for _, part := range p.glob("xl/pivotCache", "pivotCacheRecords") {
		n := 0
		walk(p.data(part), func(se xml.StartElement, _ []string) {
			if se.Name.Local == "r" {
				n++
			}
		}, nil)
		if n > 0 {
			r.hidden = append(r.hidden, passage{fmt.Sprintf("a pivot table cache holding a full copy of %d source rows", n), "", ""})
		}
	}
	for _, part := range p.glob("xl", "comments") {
		r.excelComments(p, part)
	}
	for _, part := range p.glob("xl/threadedComments", "threadedComment") {
		r.commentParts(p, part, "threadedComment", "personId")
	}
	walk(p.data("xl/persons/person.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "person" {
			r.people = append(r.people, attr(se, "displayName"))
			if id := attr(se, "userId"); emails.MatchString(id) {
				r.people = append(r.people, id)
			}
		}
	}, nil)
}

func (r *report) excelComments(p *pkg, part string) {
	var authors []string
	var cur *passage
	walk(p.data(part), func(se xml.StartElement, stack []string) {
		if se.Name.Local == "comment" {
			who := ""
			if i, err := strconv.Atoi(attr(se, "authorId")); err == nil && i >= 0 && i < len(authors) {
				who = authors[i]
			}
			r.comments = append(r.comments, passage{Where: part, Author: who})
			cur = &r.comments[len(r.comments)-1]
		}
	}, func(s string, stack []string) {
		switch {
		case top(stack) == "author":
			authors = append(authors, s)
		case cur != nil && top(stack) == "t":
			cur.Text += s
		}
	})
}

func relID(se xml.StartElement) string {
	for _, a := range se.Attr {
		if a.Name.Local == "id" && strings.Contains(a.Name.Space, "relationships") {
			return a.Value
		}
	}
	return attr(se, "id")
}

func workbookTargets(p *pkg) map[string]string {
	out := map[string]string{}
	for _, rel := range p.rels("xl/_rels/workbook.xml.rels") {
		t := strings.TrimPrefix(rel.Target, "/")
		if !strings.HasPrefix(t, "xl/") {
			t = path.Join("xl", t)
		}
		out[rel.ID] = t
	}
	return out
}

func sharedStrings(p *pkg) []string {
	var out []string
	var cur strings.Builder
	open := false
	walk(p.data("xl/sharedStrings.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "si" {
			if open {
				out = append(out, cur.String())
			}
			cur.Reset()
			open = true
		}
	}, func(s string, stack []string) {
		if top(stack) == "t" && !in(stack, "rPh") {
			cur.WriteString(s)
		}
	})
	if open {
		out = append(out, cur.String())
	}
	return out
}

// sheetCells returns the non-empty cell values of a worksheet, limited to
// the given rows when rows is not nil.
func sheetCells(b []byte, strs []string, rows map[string]bool) []string {
	var out []string
	var typ string
	keep := rows == nil
	walk(b, func(se xml.StartElement, _ []string) {
		switch se.Name.Local {
		case "row":
			keep = rows == nil || rows[attr(se, "r")]
		case "c":
			typ = attr(se, "t")
		}
	}, func(s string, stack []string) {
		if !keep || strings.TrimSpace(s) == "" {
			return
		}
		switch top(stack) {
		case "v":
			if typ == "s" {
				if i, err := strconv.Atoi(s); err == nil && i >= 0 && i < len(strs) {
					s = strs[i]
				}
			}
			out = append(out, s)
		case "t":
			if in(stack, "is") {
				out = append(out, s)
			}
		}
	})
	return out
}

func (r *report) powerpoint(p *pkg) {
	if p.parts["ppt/presentation.xml"] == nil {
		return
	}
	for _, part := range p.glob("ppt/slides", "slide") {
		hidden := false
		var text strings.Builder
		walk(p.data(part), func(se xml.StartElement, stack []string) {
			if se.Name.Local == "sld" && attr(se, "show") == "0" {
				hidden = true
			}
		}, func(s string, stack []string) {
			if top(stack) == "t" {
				text.WriteString(s + " ")
			}
		})
		if hidden {
			r.hidden = append(r.hidden, passage{"hidden slide " + strings.TrimSuffix(path.Base(part), ".xml"), "", strings.TrimSpace(text.String())})
		}
	}
	r.offSlide(p)
	var notes []string
	for _, part := range p.glob("ppt/notesSlides", "notesSlide") {
		var text strings.Builder
		placeholder := ""
		walk(p.data(part), func(se xml.StartElement, _ []string) {
			if se.Name.Local == "ph" {
				placeholder = attr(se, "type")
			}
		}, func(s string, stack []string) {
			if top(stack) == "t" && placeholder == "body" {
				text.WriteString(s)
			}
		})
		if t := strings.TrimSpace(text.String()); t != "" {
			notes = append(notes, t)
		}
	}
	if len(notes) > 0 {
		r.hidden = append(r.hidden, passage{fmt.Sprintf("speaker notes on %d slide%s", len(notes), plural(len(notes))), "", strings.Join(notes, " | ")})
		r.hiddenParts["speaker-notes.txt"] = []byte(strings.Join(notes, "\n\n") + "\n")
	}
	authors := map[string]string{}
	walk(p.data("ppt/commentAuthors.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "cmAuthor" {
			authors[attr(se, "id")] = attr(se, "name")
			r.people = append(r.people, attr(se, "name"))
		}
	}, nil)
	walk(p.data("ppt/authors.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "author" {
			authors[attr(se, "id")] = attr(se, "name")
			r.people = append(r.people, attr(se, "name"))
			if id := attr(se, "userId"); emails.MatchString(id) {
				r.people = append(r.people, id)
			}
		}
	}, nil)
	var cps []string
	cps = append(cps, p.glob("ppt/comments", "comment")...)
	cps = append(cps, p.glob("ppt/comments", "modernComment")...)
	for _, part := range cps {
		var cur *passage
		walk(p.data(part), func(se xml.StartElement, _ []string) {
			if se.Name.Local == "cm" {
				r.comments = append(r.comments, passage{Where: part, Author: authors[firstNonEmpty(attr(se, "authorId"), attr(se, "authorIdx"))]})
				cur = &r.comments[len(r.comments)-1]
			}
		}, func(s string, stack []string) {
			if cur != nil && (top(stack) == "text" || top(stack) == "t") {
				cur.Text += s
			}
		})
	}
}

// links finds external relationships that point into someone's disk or
// network shares: attached templates, linked workbooks, linked images.
func (r *report) links(p *pkg) {
	for _, name := range p.relParts() {
		for _, rel := range p.rels(name) {
			if rel.TargetMode == "External" {
				r.paths = append(r.paths, findPaths(rel.Target)...)
			}
		}
	}
}

func (r *report) media(p *pkg) {
	var names []string
	for name := range p.parts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dir := path.Base(path.Dir(name))
		switch {
		case dir == "embeddings":
			r.embedded = append(r.embedded, fmt.Sprintf("%s (%s)", path.Base(name), size(p.parts[name].UncompressedSize64)))
		case path.Base(name) == "vbaProject.bin":
			r.embedded = append(r.embedded, fmt.Sprintf("a VBA macro project (%s)", size(p.parts[name].UncompressedSize64)))
		case dir == "media" || strings.HasPrefix(name, "docProps/thumbnail"):
			r.nestedImage(p, name)
		}
	}
}

// nestedRules are the image leaks worth reporting from inside a document.
// A picture's capture date or camera model adds noise to every report
// about a document with photos in it, and Office thumbnails routinely
// carry stale bytes after their end (12 of the 603 Office files in the
// calibration corpus did).
var nestedRules = map[string]bool{
	leak.HiddenCrop.ID: true, leak.GPSLocation.ID: true, leak.ThumbnailMismatch.ID: true,
	leak.HiddenVideo.ID: true, leak.DeviceSerial.ID: true, leak.PersonalNames.ID: true,
}

// nestedImage inspects a picture stored inside the document.
func (r *report) nestedImage(p *pkg, name string) {
	b := p.data(name)
	var fs []leak.Finding
	switch sniff.Detect(b) {
	case sniff.JPEG:
		fs, _ = jpegfile.Inspect(b)
	case sniff.PNG:
		fs, _ = pngfile.Inspect(b)
	}
	for _, f := range fs {
		if nestedRules[f.Rule] {
			f.Message = "A picture inside the document: " + lowerFirst(f.Message)
			r.nested = append(r.nested, f.At(name))
		}
	}
}

var pathLike = regexp.MustCompile(`(?i)(?:file:///?[^\s"<>]+|\b[a-z]:\\[^"<>]*|/(?:Users|home)/[^\s"<>/]+[^"<>]*|\\\\[a-z0-9_.-]+\\[^"<>]*)`)

func findPaths(s string) []string { return pathLike.FindAllString(strings.TrimSpace(s), -1) }

func describeHidden(ps []passage) string {
	var kinds []string
	for _, p := range ps {
		kinds = append(kinds, p.Where)
	}
	kinds = uniq(kinds...)
	if len(kinds) > 4 {
		kinds = append(kinds[:4], fmt.Sprintf("%d more", len(kinds)-4))
	}
	return strings.Join(kinds, ", ")
}

func byAuthors(ps []passage) string {
	var names []string
	for _, p := range ps {
		names = append(names, p.Author)
	}
	names = uniq(names...)
	if len(names) == 0 {
		return ""
	}
	return " (by " + strings.Join(names, ", ") + ")"
}

func samples(ps []passage) string {
	var q []string
	for _, p := range ps {
		if strings.TrimSpace(p.Text) != "" {
			q = append(q, strconv.Quote(truncate(strings.Join(strings.Fields(p.Text), " "), 80)))
		}
		if len(q) == 4 {
			break
		}
	}
	return strings.Join(q, ", ")
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

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < ' ' {
			return '_'
		}
		return r
	}, s)
}

func minutes(m int) string {
	if m < 120 {
		return fmt.Sprintf("%d minutes", m)
	}
	return fmt.Sprintf("%d hours", m/60)
}

func size(n uint64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
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

// hiddenColumns returns the values in columns marked hidden, and how many
// such columns hold data.
func hiddenColumns(b []byte, strs []string) ([]string, int) {
	hidden := map[int]bool{}
	walk(b, func(se xml.StartElement, stack []string) {
		if se.Name.Local == "col" && (attr(se, "hidden") == "1" || attr(se, "hidden") == "true") {
			lo, err1 := strconv.Atoi(attr(se, "min"))
			hi, err2 := strconv.Atoi(attr(se, "max"))
			if err1 == nil && err2 == nil && hi >= lo && hi-lo < 1024 {
				for c := lo; c <= hi; c++ {
					hidden[c] = true
				}
			}
		}
	}, nil)
	if len(hidden) == 0 {
		return nil, 0
	}
	var out []string
	used := map[int]bool{}
	var col int
	var typ string
	walk(b, func(se xml.StartElement, _ []string) {
		if se.Name.Local == "c" {
			col, typ = column(attr(se, "r")), attr(se, "t")
		}
	}, func(s string, stack []string) {
		if !hidden[col] || strings.TrimSpace(s) == "" || !in(stack, "c") {
			return
		}
		switch top(stack) {
		case "v":
			if typ == "s" {
				if i, err := strconv.Atoi(s); err == nil && i >= 0 && i < len(strs) {
					s = strs[i]
				}
			}
		case "t":
		default:
			return
		}
		used[col] = true
		out = append(out, s)
	})
	return out, len(used)
}

// column turns a cell reference such as "AB12" into its 1-based column.
func column(ref string) int {
	n := 0
	for _, c := range ref {
		if c < 'A' || c > 'Z' {
			break
		}
		n = n*26 + int(c-'A'+1)
	}
	return n
}

// offSlide finds text boxes placed entirely outside the slide: invisible in
// a slideshow and in exports to PDF, but part of the file.
func (r *report) offSlide(p *pkg) {
	var w, h int64
	walk(p.data("ppt/presentation.xml"), func(se xml.StartElement, _ []string) {
		if se.Name.Local == "sldSz" {
			w, _ = strconv.ParseInt(attr(se, "cx"), 10, 64)
			h, _ = strconv.ParseInt(attr(se, "cy"), 10, 64)
		}
	}, nil)
	if w <= 0 || h <= 0 {
		return
	}
	for _, part := range p.glob("ppt/slides", "slide") {
		var x, y, cx, cy int64
		var text strings.Builder
		depth := 0
		flush := func() {
			outside := x+cx <= 0 || y+cy <= 0 || x >= w || y >= h
			if t := strings.TrimSpace(text.String()); outside && t != "" && (cx > 0 || cy > 0) {
				r.invisible = append(r.invisible, passage{"outside the slide", "", t})
			}
			text.Reset()
			x, y, cx, cy = 0, 0, 0, 0
		}
		walk(p.data(part), func(se xml.StartElement, stack []string) {
			// Only shapes directly in the slide tree: shapes inside groups
			// use the group's coordinate space.
			top := len(stack) >= 2 && stack[len(stack)-2] == "spTree"
			switch {
			case se.Name.Local == "sp" && top:
				flush()
				depth = len(stack)
			case se.Name.Local == "off" && in(stack, "spPr") && len(stack) == depth+3:
				x, _ = strconv.ParseInt(attr(se, "x"), 10, 64)
				y, _ = strconv.ParseInt(attr(se, "y"), 10, 64)
			case se.Name.Local == "ext" && in(stack, "spPr") && len(stack) == depth+3:
				cx, _ = strconv.ParseInt(attr(se, "cx"), 10, 64)
				cy, _ = strconv.ParseInt(attr(se, "cy"), 10, 64)
			}
		}, func(s string, stack []string) {
			if depth > 0 && len(stack) > depth && stack[len(stack)-1] == "t" {
				text.WriteString(s + " ")
			}
		})
		flush()
	}
}

// printers reads the device name at the start of the DEVMODE structures
// Office stores for each printer a document was set up for. A name such as
// \\PRINT-SRV\HR-3rd-floor names a server and a place.
func (r *report) printers(p *pkg) {
	for name := range p.parts {
		if !strings.Contains(name, "printerSettings") || !strings.HasSuffix(name, ".bin") {
			continue
		}
		b := p.data(name)
		if len(b) < 64 {
			continue
		}
		u := make([]uint16, 0, 32)
		for i := 0; i+1 < 64; i += 2 {
			c := uint16(b[i]) | uint16(b[i+1])<<8
			if c == 0 {
				break
			}
			u = append(u, c)
		}
		dev := strings.TrimSpace(string(utf16.Decode(u)))
		switch {
		case dev == "":
		case strings.HasPrefix(dev, "\\\\"):
			r.paths = append(r.paths, dev)
		default:
			r.trail = append(r.trail, "printer: "+dev)
		}
	}
}
