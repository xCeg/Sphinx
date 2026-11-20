package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"path"
	"strings"

	"github.com/xceg/sphinx/internal/jpegfile"
	"github.com/xceg/sphinx/internal/pngfile"
	"github.com/xceg/sphinx/internal/sniff"
)

const (
	appXML    = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"></Properties>`
	customXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties"></Properties>`
)

// Seal rewrites document properties, strips local paths from external
// links and seals embedded pictures. Tracked changes, comments and hidden
// content are left alone: deciding what a document says is the author's
// job, so they stay in the report after sealing.
func Seal(b []byte) ([]byte, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range p.zr.File {
		name := strings.TrimPrefix(f.Name, "/")
		data := p.data(name)
		switch {
		case name == "docProps/core.xml":
			data = coreXML(data)
		case name == "docProps/app.xml":
			data = []byte(appXML)
		case name == "docProps/custom.xml":
			data = []byte(customXML)
		case strings.HasSuffix(name, ".rels"):
			data = stripPaths(data)
		case path.Base(path.Dir(name)) == "media" || strings.HasPrefix(name, "docProps/thumbnail"):
			data = sealImage(data)
		}
		hdr := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified}
		if f.Method == zip.Store {
			hdr.Method = zip.Store
		}
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// coreXML keeps only the title, which the author chose to show.
func coreXML(b []byte) []byte {
	var title string
	walk(b, nil, func(s string, stack []string) {
		if top(stack) == "title" {
			title += s
		}
	})
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	sb.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">`)
	if title != "" {
		sb.WriteString("<dc:title>")
		xml.EscapeText(&sb, []byte(title))
		sb.WriteString("</dc:title>")
	}
	sb.WriteString("</cp:coreProperties>")
	return []byte(sb.String())
}

// stripPaths replaces external targets that point into a disk or share
// with their bare file name, which keeps the link meaningful without
// revealing whose machine it came from.
func stripPaths(b []byte) []byte {
	var r relationships
	if err := xml.Unmarshal(b, &r); err != nil {
		return b
	}
	changed := false
	for i, rel := range r.Rels {
		if rel.TargetMode == "External" && len(findPaths(rel.Target)) > 0 {
			t := strings.ReplaceAll(rel.Target, "\\", "/")
			r.Rels[i].Target = path.Base(t)
			changed = true
		}
	}
	if !changed {
		return b
	}
	out, err := xml.Marshal(r)
	if err != nil {
		return b
	}
	return append([]byte(xml.Header), out...)
}

func sealImage(b []byte) []byte {
	var sealed []byte
	var err error
	switch sniff.Detect(b) {
	case sniff.JPEG:
		sealed, err = jpegfile.Seal(b)
	case sniff.PNG:
		sealed, err = pngfile.Seal(b)
	default:
		return b
	}
	if err != nil {
		return b
	}
	return sealed
}
