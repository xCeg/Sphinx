// Package render prints inspection results as text, JSON, SARIF or Markdown.
package render

import (
	"fmt"
	"io"

	"github.com/xceg/sphinx"
)

// Report is the result for one file.
type Report struct {
	Path     string           `json:"path"`
	Format   string           `json:"format,omitempty"`
	Size     int64            `json:"size"`
	Verdict  sphinx.Verdict   `json:"verdict,omitempty"`
	Findings []sphinx.Finding `json:"findings"`
	Error    string           `json:"error,omitempty"`
}

type Options struct {
	Color   bool
	Quiet   bool
	Version string
}

type Renderer func(w io.Writer, reports []Report, o Options) error

var Formats = map[string]Renderer{
	"text":     Text,
	"json":     JSON,
	"sarif":    SARIF,
	"markdown": Markdown,
}

func Lookup(name string) (Renderer, error) {
	r, ok := Formats[name]
	if !ok {
		return nil, fmt.Errorf("unknown format %q (want text, json, sarif or markdown)", name)
	}
	return r, nil
}

var formatNames = map[string]string{
	"png": "PNG image", "jpeg": "JPEG image", "pdf": "PDF document",
	"docx": "Word document", "xlsx": "Excel workbook", "pptx": "PowerPoint presentation",
	"webp": "WebP image", "svg": "SVG drawing", "zip": "zip archive", "heif": "HEIF photo", "mp4": "video", "tiff": "TIFF image", "gif": "GIF image", "odt": "OpenDocument text", "ods": "OpenDocument spreadsheet", "odp": "OpenDocument presentation",
}

func FormatName(f string) string {
	if n, ok := formatNames[f]; ok {
		return n
	}
	return f
}
