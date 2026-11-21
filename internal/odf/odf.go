// Package odf inspects, reveals and seals OpenDocument files (LibreOffice
// and OpenOffice text, spreadsheet and presentation documents).
package odf

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xceg/sphinx/internal/aitext"
	"github.com/xceg/sphinx/internal/jpegfile"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/pngfile"
	"github.com/xceg/sphinx/internal/sniff"
)

const maxPart = 64 << 20

type pkg struct {
	zr    *zip.Reader
	parts map[string]*zip.File
}

var errNotODF = errors.New("not an OpenDocument file")

func open(b []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("damaged zip container: %w", err)
	}
	p := &pkg{zr: zr, parts: map[string]*zip.File{}}
	for _, f := range zr.File {
		p.parts[f.Name] = f
	}
	if p.parts["content.xml"] == nil && p.parts["meta.xml"] == nil {
		return nil, errNotODF
	}
	return p, nil
}

func (p *pkg) data(name string) []byte {
	f := p.parts[name]
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, maxPart))
	return b
}

type passage struct{ where, author, text string }

type report struct {
	deleted, comments, hidden []passage
	people, paths, trail      []string
	when                      []string
	embedded                  []string
	nested                    []leak.Finding
}

// walk streams an XML part, reporting start elements and text with the
// stack of qualified names ("prefix:local") above them.
func walk(b []byte, visit func(se xml.StartElement, stack []string), text func(s string, stack []string)) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	var stack []string
	for {
		tok, err := d.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, prefixed(t.Name))
			if visit != nil {
				visit(t, stack)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if text != nil && len(stack) > 0 {
				text(string(t), stack)
			}
		}
	}
}

var prefixes = map[string]string{
	"urn:oasis:names:tc:opendocument:xmlns:text:1.0":         "text",
	"urn:oasis:names:tc:opendocument:xmlns:office:1.0":       "office",
	"urn:oasis:names:tc:opendocument:xmlns:meta:1.0":         "meta",
	"urn:oasis:names:tc:opendocument:xmlns:table:1.0":        "table",
	"urn:oasis:names:tc:opendocument:xmlns:style:1.0":        "style",
	"urn:oasis:names:tc:opendocument:xmlns:drawing:1.0":      "draw",
	"urn:oasis:names:tc:opendocument:xmlns:presentation:1.0": "presentation",
	"urn:oasis:names:tc:opendocument:xmlns:config:1.0":       "config",
	"http://purl.org/dc/elements/1.1/":                       "dc",
	"http://www.w3.org/1999/xlink":                           "xlink",
}

func prefixed(n xml.Name) string {
	if p, ok := prefixes[n.Space]; ok {
		return p + ":" + n.Local
	}
	return n.Local
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if prefixed(a.Name) == name {
			return a.Value
		}
	}
	return ""
}

func in(stack []string, name string) bool {
	for _, s := range stack {
		if s == name {
			return true
		}
	}
	return false
}

func top(stack []string) string { return stack[len(stack)-1] }

func Inspect(b []byte) ([]leak.Finding, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	return analyze(p).findings(), nil
}

func analyze(p *pkg) *report {
	r := &report{}
	r.meta(p.data("meta.xml"))
	r.content(p.data("content.xml"), p.data("styles.xml"))
	r.settings(p.data("settings.xml"))
	r.files(p)
	return r
}

var pathLike = regexp.MustCompile(`(?i)(?:file:///?[^\s"<>]+|\b[a-z]:\\[^"<>]*|/(?:Users|home)/[^\s"<>/]+[^"<>]*|\\\\[a-z0-9_.-]+\\[^"<>]*)`)

func (r *report) meta(b []byte) {
	var field string
	walk(b, func(se xml.StartElement, _ []string) {
		switch prefixed(se.Name) {
		case "meta:template":
			r.paths = append(r.paths, pathLike.FindAllString(attr(se, "xlink:href"), -1)...)
		case "meta:user-defined":
			field = attr(se, "meta:name")
		}
	}, func(s string, stack []string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		switch top(stack) {
		case "meta:initial-creator":
			r.people = append(r.people, "author: "+s)
		case "dc:creator":
			r.people = append(r.people, "last saved by: "+s)
		case "meta:printed-by":
			r.people = append(r.people, "printed by: "+s)
		case "meta:user-defined":
			// Custom properties hold anything from a company name to a
			// version number; only text that is not a number is kept.
			if _, err := strconv.ParseFloat(s, 64); err != nil {
				r.people = append(r.people, field+": "+s)
			}
		case "meta:creation-date":
			r.when = append(r.when, "created "+s)
		case "dc:date":
			r.when = append(r.when, "modified "+s)
		case "meta:print-date":
			r.when = append(r.when, "printed "+s)
		case "meta:generator":
			r.trail = append(r.trail, s)
		case "meta:editing-cycles":
			if n, _ := strconv.Atoi(s); n > 1 {
				r.trail = append(r.trail, "saved "+s+" times")
			}
		case "meta:editing-duration":
			r.trail = append(r.trail, "edited for "+strings.ToLower(strings.TrimPrefix(s, "PT")))
		}
	})
}

// hiddenStyles names the automatic styles that hide a sheet, a row, a
// column, a section or a slide.
func hiddenStyles(parts ...[]byte) map[string]bool {
	out := map[string]bool{}
	for _, b := range parts {
		var cur string
		walk(b, func(se xml.StartElement, _ []string) {
			switch prefixed(se.Name) {
			case "style:style":
				cur = attr(se, "style:name")
			case "style:table-properties":
				if attr(se, "table:display") == "false" {
					out[cur] = true
				}
			case "style:drawing-page-properties":
				if attr(se, "presentation:visibility") == "hidden" {
					out[cur] = true
				}
			}
		}, nil)
	}
	return out
}

func (r *report) content(b, styles []byte) {
	hidden := hiddenStyles(b, styles)
	var cur *passage
	var section []string
	flushSection := func() {
		if len(section) > 0 {
			if t := strings.Join(strings.Fields(strings.Join(section[1:], " ")), " "); t != "" {
				r.hidden = append(r.hidden, passage{where: "hidden " + section[0], text: t})
			}
		}
	}
	hiddenDepth := 0
	walk(b, func(se xml.StartElement, stack []string) {
		name := prefixed(se.Name)
		switch name {
		case "text:deletion":
			r.deleted = append(r.deleted, passage{where: "content.xml"})
			cur = &r.deleted[len(r.deleted)-1]
		case "office:annotation":
			r.comments = append(r.comments, passage{where: "content.xml"})
			cur = &r.comments[len(r.comments)-1]
		case "text:hidden-text", "text:hidden-paragraph":
			if attr(se, "text:is-hidden") != "false" {
				r.hidden = append(r.hidden, passage{where: "hidden text", text: attr(se, "text:string-value")})
			}
		}
		kind := ""
		switch {
		case name == "table:table" && hidden[attr(se, "table:style-name")]:
			kind = "sheet " + strconv.Quote(attr(se, "table:name"))
		case (name == "table:table-row" || name == "table:table-column") && (attr(se, "table:visibility") == "collapse" || attr(se, "table:visibility") == "filter"):
			kind = strings.TrimPrefix(name, "table:table-")
		case name == "text:section" && attr(se, "text:display") == "none":
			kind = "section"
		case name == "draw:page" && hidden[attr(se, "draw:style-name")]:
			kind = "slide " + strconv.Quote(attr(se, "draw:name"))
		case name == "presentation:notes":
			kind = "speaker notes"
		}
		if kind != "" && hiddenDepth == 0 {
			hiddenDepth = len(stack)
			section = []string{kind}
		}
	}, func(s string, stack []string) {
		if hiddenDepth > 0 && len(stack) < hiddenDepth {
			flushSection()
			hiddenDepth, section = 0, nil
		}
		switch {
		case cur != nil && top(stack) == "dc:creator":
			cur.author = strings.TrimSpace(s)
		case cur != nil && (in(stack, "text:deletion") || in(stack, "office:annotation")) && top(stack) != "dc:date":
			cur.text += s
		case hiddenDepth > 0 && len(stack) >= hiddenDepth:
			section = append(section, s)
		}
	})
	if hiddenDepth > 0 {
		flushSection()
	}
}

func (r *report) settings(b []byte) {
	var name string
	walk(b, func(se xml.StartElement, _ []string) {
		if prefixed(se.Name) == "config:config-item" {
			name = attr(se, "config:name")
		}
	}, func(s string, stack []string) {
		s = strings.TrimSpace(s)
		if top(stack) != "config:config-item" || s == "" || name != "PrinterName" {
			return
		}
		if strings.HasPrefix(s, `\\`) {
			r.paths = append(r.paths, s)
		} else {
			r.trail = append(r.trail, "printer: "+s)
		}
	})
}

var nestedRules = map[string]bool{
	leak.HiddenCrop.ID: true, leak.GPSLocation.ID: true, leak.ThumbnailMismatch.ID: true,
	leak.HiddenVideo.ID: true, leak.DeviceSerial.ID: true, leak.PersonalNames.ID: true,
}

func (r *report) files(p *pkg) {
	var names []string
	for n := range p.parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "Object ") && path.Base(n) == "content.xml":
			r.embedded = append(r.embedded, path.Dir(n))
		case strings.HasPrefix(n, "Basic/") && strings.HasSuffix(n, ".xml") && path.Base(n) != "script-lc.xml" && path.Base(n) != "dialog-lc.xml" && path.Base(n) != "script-lb.xml":
			r.embedded = append(r.embedded, "Basic macro "+n)
		case strings.HasPrefix(n, "Pictures/"):
			b := p.data(n)
			var fs []leak.Finding
			switch sniff.Detect(b) {
			case sniff.JPEG:
				fs, _ = jpegfile.Inspect(b)
			case sniff.PNG:
				fs, _ = pngfile.Inspect(b)
			}
			for _, f := range fs {
				if nestedRules[f.Rule] {
					f.Message = "A picture inside the document: " + strings.ToLower(f.Message[:1]) + f.Message[1:]
					r.nested = append(r.nested, f.At(n))
				}
			}
		}
	}
}

func (r *report) findings() []leak.Finding {
	var out []leak.Finding
	if n := len(r.deleted); n > 0 {
		out = append(out, leak.DeletedText.Find(fmt.Sprintf("%d deleted passage%s %s still stored as tracked changes%s.", n, plural(n), isAre(n), byAuthors(r.deleted)), samples(r.deleted)))
	}
	if len(r.hidden) > 0 {
		var kinds []string
		var all strings.Builder
		for _, h := range r.hidden {
			kinds = append(kinds, h.where)
			all.WriteString(h.text + "\n")
		}
		if hit := aitext.Match(all.String()); hit != "" {
			out = append(out, leak.HiddenInstructions.Find("Text hidden from human readers gives instructions to the AI tools that will read this document.", strconv.Quote(hit)))
		}
		out = append(out, leak.HiddenContent.Find(fmt.Sprintf("The file holds content that is not visible when you read it: %s.", strings.Join(uniq(kinds), ", ")), samples(r.hidden)))
	}
	if n := len(r.comments); n > 0 {
		out = append(out, leak.Comments.Find(fmt.Sprintf("%d comment%s%s %s stored in the file.", n, plural(n), byAuthors(r.comments), isAre(n)), samples(r.comments)))
	}
	if v := uniq(r.people); len(v) > 0 {
		out = append(out, leak.PersonalNames.Find("The file names the people and organisation who created and edited it.", strings.Join(v, "; ")))
	}
	if v := uniq(r.paths); len(v) > 0 {
		out = append(out, leak.LocalPaths.Find("The file contains paths from the computer it was made on.", strings.Join(v, "; ")))
	}
	if n := len(r.embedded); n > 0 {
		out = append(out, leak.EmbeddedFiles.Find(fmt.Sprintf("%d embedded object%s travel%s with the file.", n, plural(n), verbS(n)), strings.Join(r.embedded, "; ")))
	}
	out = append(out, r.nested...)
	if v := uniq(r.trail); len(v) > 0 {
		out = append(out, leak.SoftwareTrail.Find("The file records the software and editing history behind it.", strings.Join(v, "; ")))
	}
	if v := uniq(r.when); len(v) > 0 {
		out = append(out, leak.Timestamps.Find("The file records when it was created, edited or printed.", strings.Join(v, "; ")))
	}
	return out
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	r := analyze(p)
	var out []leak.Artifact
	text := func(name, note string, ps []passage) {
		if len(ps) == 0 {
			return
		}
		var sb strings.Builder
		for _, x := range ps {
			if x.author != "" {
				fmt.Fprintf(&sb, "%s: %s\n", x.author, strings.TrimSpace(x.text))
			} else {
				fmt.Fprintf(&sb, "[%s] %s\n", x.where, strings.TrimSpace(x.text))
			}
		}
		out = append(out, leak.Artifact{Name: name, Note: note, Data: []byte(sb.String())})
	}
	text("deleted-text.txt", "text deleted under tracked changes", r.deleted)
	text("comments.txt", "comments with their authors", r.comments)
	text("hidden.txt", "content hidden from view", r.hidden)
	return out, nil
}

// Seal rewrites meta.xml keeping the title, blanks the printer name and
// seals pictures. The mimetype entry stays first and uncompressed, as the
// format requires. Tracked changes, comments and hidden content are left.
func Seal(b []byte) ([]byte, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range p.zr.File {
		data := p.data(f.Name)
		switch {
		case f.Name == "meta.xml":
			data = metaXML(data)
		case f.Name == "settings.xml":
			data = printerName.ReplaceAll(data, []byte(`$1$2`))
		case strings.HasPrefix(f.Name, "Pictures/") || strings.HasPrefix(f.Name, "Thumbnails/"):
			data = sealImage(data)
		}
		if f.Name == "mimetype" {
			// Readers find the type at a fixed offset, so the entry must be
			// stored, without compression or a trailing data descriptor.
			hdr := &zip.FileHeader{Name: f.Name, Method: zip.Store, Modified: f.Modified,
				CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data))}
			fw, err := w.CreateRaw(hdr)
			if err != nil {
				return nil, err
			}
			fw.Write(data)
			continue
		}
		fw, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
		if err != nil {
			return nil, err
		}
		fw.Write(data)
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var printerName = regexp.MustCompile(`(<config:config-item config:name="Printer(?:Name|Setup)"[^>]*>)[^<]*(</config:config-item>)`)

func metaXML(b []byte) []byte {
	var title string
	walk(b, nil, func(s string, stack []string) {
		if top(stack) == "dc:title" {
			title += s
		}
	})
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<office:document-meta xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:dc="http://purl.org/dc/elements/1.1/" office:version="1.3"><office:meta>`)
	if title != "" {
		sb.WriteString("<dc:title>")
		xml.EscapeText(&sb, []byte(title))
		sb.WriteString("</dc:title>")
	}
	sb.WriteString("</office:meta></office:document-meta>")
	return []byte(sb.String())
}

func sealImage(b []byte) []byte {
	var out []byte
	var err error
	switch sniff.Detect(b) {
	case sniff.JPEG:
		out, err = jpegfile.Seal(b)
	case sniff.PNG:
		out, err = pngfile.Seal(b)
	default:
		return b
	}
	if err != nil {
		return b
	}
	return out
}

func byAuthors(ps []passage) string {
	var names []string
	for _, p := range ps {
		names = append(names, p.author)
	}
	if names = uniq(names); len(names) == 0 {
		return ""
	}
	return " (by " + strings.Join(names, ", ") + ")"
}

func samples(ps []passage) string {
	var q []string
	for _, p := range ps {
		if t := strings.Join(strings.Fields(p.text), " "); t != "" {
			if len([]rune(t)) > 80 {
				t = string([]rune(t)[:80]) + "…"
			}
			q = append(q, strconv.Quote(t))
		}
		if len(q) == 4 {
			break
		}
	}
	return strings.Join(q, ", ")
}

func uniq(vs []string) []string {
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
