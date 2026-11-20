// Package xmp flattens an XMP packet into prefix:name -> values, with
// prefixes canonicalised from namespace URIs so that a packet written as
// <rdf:Description exif:GPSLatitude="..."/> and one written with nested
// elements read the same.
package xmp

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

var prefixes = map[string]string{
	"http://purl.org/dc/elements/1.1/":                 "dc",
	"http://ns.adobe.com/xap/1.0/":                     "xmp",
	"http://ns.adobe.com/xap/1.0/mm/":                  "xmpMM",
	"http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": "stEvt",
	"http://ns.adobe.com/xap/1.0/sType/ResourceRef#":   "stRef",
	"http://ns.adobe.com/exif/1.0/":                    "exif",
	"http://ns.adobe.com/exif/1.0/aux/":                "aux",
	"http://ns.adobe.com/tiff/1.0/":                    "tiff",
	"http://ns.adobe.com/photoshop/1.0/":               "photoshop",
	"http://ns.adobe.com/pdf/1.3/":                     "pdf",
	"http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/":      "Iptc4xmpCore",
	"http://ns.google.com/photos/1.0/camera/":          "GCamera",
	"http://ns.google.com/photos/1.0/container/":       "Container",
	"http://ns.google.com/photos/1.0/container/item/":  "Item",
	"http://cipa.jp/exif/1.0/":                         "exifEX",
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#":      "rdf",
	"http://ns.microsoft.com/photo/1.0/":               "MicrosoftPhoto",
	"http://ns.adobe.com/camera-raw-settings/1.0/":     "crs",
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":        "xmpDM",
	"http://ns.adobe.com/xap/1.0/rights/":              "xmpRights",
	"http://www.npes.org/pdfx/ns/id/":                  "pdfx",
	"http://ns.adobe.com/pdfx/1.3/":                    "pdfx",
	"http://ns.apple.com/faceinfo/1.0/":                "apple-fi",
	"http://ns.google.com/photos/dd/1.0/device/":       "Device",
}

type Packet map[string][]string

func (p Packet) First(key string) string {
	if v := p[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func name(n xml.Name) string {
	p, ok := prefixes[n.Space]
	if !ok {
		p = n.Space
		if i := strings.LastIndexAny(strings.TrimRight(p, "/#"), "/#"); i >= 0 {
			p = strings.TrimRight(p, "/#")[i+1:]
		}
	}
	return p + ":" + n.Local
}

// Parse extracts the packet from b, which may contain bytes before and after
// the <x:xmpmeta> or <rdf:RDF> element.
func Parse(b []byte) Packet {
	out := Packet{}
	start := bytes.Index(b, []byte("<x:xmpmeta"))
	if start < 0 {
		start = bytes.Index(b, []byte("<rdf:RDF"))
	}
	if start < 0 {
		return out
	}
	d := xml.NewDecoder(bytes.NewReader(b[start:]))
	d.Strict = false
	var stack []string
	var text strings.Builder
	for {
		tok, err := d.Token()
		if err == io.EOF || err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := name(t.Name)
			stack = append(stack, n)
			text.Reset()
			for _, a := range t.Attr {
				an := name(a.Name)
				if a.Name.Space == "xmlns" || a.Name.Space == "" || strings.HasPrefix(an, "rdf:") {
					continue
				}
				if v := strings.TrimSpace(a.Value); v != "" {
					out[an] = append(out[an], v)
				}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if len(stack) == 0 {
				return out
			}
			v := strings.TrimSpace(text.String())
			text.Reset()
			key := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if v == "" {
				continue
			}
			if key == "rdf:li" {
				key = property(stack)
			}
			if key != "" && !strings.HasPrefix(key, "rdf:") && !strings.HasPrefix(key, "x:") {
				out[key] = append(out[key], v)
			}
		}
	}
}

// property returns the nearest enclosing element that is not RDF syntax.
func property(stack []string) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if !strings.HasPrefix(stack[i], "rdf:") {
			return stack[i]
		}
	}
	return ""
}
