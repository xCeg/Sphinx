// Package ooxml inspects, reveals and seals Word, Excel and PowerPoint
// files (the zip-based Office Open XML formats).
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Limits against zip bombs: a part may expand to at most maxPart bytes and
// the whole package to maxTotal.
const (
	maxPart  = 64 << 20
	maxTotal = 512 << 20
)

type pkg struct {
	zr    *zip.Reader
	parts map[string]*zip.File
	read  int64
}

var errNotOOXML = errors.New("not an Office Open XML file")

func open(b []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("damaged zip container: %w", err)
	}
	p := &pkg{zr: zr, parts: map[string]*zip.File{}}
	for _, f := range zr.File {
		p.parts[strings.TrimPrefix(f.Name, "/")] = f
	}
	if p.parts["[Content_Types].xml"] == nil {
		return nil, errNotOOXML
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
	b, _ := io.ReadAll(io.LimitReader(rc, min(maxPart, maxTotal-p.read)))
	p.read += int64(len(b))
	return b
}

// glob lists part names under dir whose base name starts with prefix,
// in natural order (slide2 before slide10).
func (p *pkg) glob(dir, prefix string) []string {
	var out []string
	for name := range p.parts {
		if path.Dir(name) == dir && strings.HasPrefix(path.Base(name), prefix) && strings.HasSuffix(name, ".xml") {
			out = append(out, name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// walk streams the elements of an XML part. visit receives each start
// element and the stack of local names above it; text receives character
// data with the same stack.
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
			stack = append(stack, t.Name.Local)
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

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
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

func top(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

type relationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr,omitempty"`
}

type relationships struct {
	XMLName xml.Name       `xml:"http://schemas.openxmlformats.org/package/2006/relationships Relationships"`
	Rels    []relationship `xml:"Relationship"`
}

func (p *pkg) rels(name string) []relationship {
	var r relationships
	if err := xml.Unmarshal(p.data(name), &r); err != nil {
		return nil
	}
	return r.Rels
}

func (p *pkg) relParts() []string {
	var out []string
	for name := range p.parts {
		if strings.HasSuffix(name, ".rels") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
