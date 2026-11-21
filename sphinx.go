// Package sphinx finds what a file gives away before you share it: pixels
// that survived a crop, text under a black box, earlier revisions, deleted
// text, GPS positions, serial numbers and names. It can also extract those
// hidden parts and write cleaned copies.
//
// Everything runs locally on the bytes it is given; nothing is sent anywhere.
package sphinx

import (
	"errors"
	"sort"

	"github.com/xceg/sphinx/internal/bmff"
	"github.com/xceg/sphinx/internal/giffile"
	"github.com/xceg/sphinx/internal/jpegfile"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/ooxml"
	"github.com/xceg/sphinx/internal/pdf"
	"github.com/xceg/sphinx/internal/pngfile"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/svgfile"
	"github.com/xceg/sphinx/internal/tifffile"
	"github.com/xceg/sphinx/internal/webpfile"
	"github.com/xceg/sphinx/internal/zipfile"
)

type (
	Finding  = leak.Finding
	Severity = leak.Severity
	Artifact = leak.Artifact
	Rule     = leak.Rule
)

const (
	Low      = leak.Low
	Medium   = leak.Medium
	High     = leak.High
	Critical = leak.Critical
)

// Rules lists everything Sphinx looks for, in reference order.
func Rules() []*Rule { return leak.Rules }

func LookupRule(idOrName string) *Rule { return leak.Lookup(idOrName) }

func ParseSeverity(s string) (Severity, error) { return leak.ParseSeverity(s) }

var ErrUnsupported = errors.New("unsupported file type")

type handler struct {
	inspect func([]byte) ([]Finding, error)
	reveal  func([]byte) ([]Artifact, error)
	seal    func([]byte) ([]byte, error)
}

var handlers = map[sniff.Format]handler{
	sniff.PNG:  {pngfile.Inspect, pngfile.Reveal, pngfile.Seal},
	sniff.JPEG: {jpegfile.Inspect, jpegfile.Reveal, jpegfile.Seal},
	sniff.PDF:  {pdf.Inspect, pdf.Reveal, pdf.Seal},
	sniff.DOCX: {ooxml.Inspect, ooxml.Reveal, ooxml.Seal},
	sniff.XLSX: {ooxml.Inspect, ooxml.Reveal, ooxml.Seal},
	sniff.PPTX: {ooxml.Inspect, ooxml.Reveal, ooxml.Seal},
	sniff.WEBP: {webpfile.Inspect, webpfile.Reveal, webpfile.Seal},
	sniff.SVG:  {svgfile.Inspect, svgfile.Reveal, svgfile.Seal},
	sniff.HEIF: {bmff.Inspect, nil, bmff.Seal},
	sniff.TIFF: {tifffile.Inspect, nil, tifffile.Seal},
	sniff.GIF:  {giffile.Inspect, nil, giffile.Seal},
	sniff.MP4:  {bmff.Inspect, nil, bmff.Seal},
}

// Archives inspect their members with the full engine, which refers back
// to this table, so they are registered once it exists.
func init() {
	e := zipfile.Engine{Inspect: Inspect, Reveal: Reveal, Seal: Seal}
	handlers[sniff.ZIP] = handler{
		inspect: func(b []byte) ([]Finding, error) { return zipfile.Inspect(b, e) },
		reveal:  func(b []byte) ([]Artifact, error) { return zipfile.Reveal(b, e) },
		seal:    func(b []byte) ([]byte, error) { return zipfile.Seal(b, e) },
	}
}

// Format names the kind of file data holds, judged by content, or "" when
// Sphinx does not support it.
func Format(data []byte) string {
	f := sniff.Detect(data)
	if _, ok := handlers[f]; !ok {
		return ""
	}
	return string(f)
}

func lookup(data []byte) (handler, error) {
	h, ok := handlers[sniff.Detect(data)]
	if !ok {
		return handler{}, ErrUnsupported
	}
	return h, nil
}

// Inspect reports what data gives away, most severe first.
func Inspect(data []byte) ([]Finding, error) {
	h, err := lookup(data)
	if err != nil {
		return nil, err
	}
	fs, err := h.inspect(data)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Severity > fs[j].Severity })
	return fs, err
}

// Reveal extracts what data hides: recovered pixels, hidden text, earlier
// revisions, embedded previews and videos.
func Reveal(data []byte) ([]Artifact, error) {
	h, err := lookup(data)
	if err != nil || h.reveal == nil {
		return nil, ErrUnsupported
	}
	return h.reveal(data)
}

// Seal returns a copy of data with the leaks Sphinx knows how to remove
// taken out. Inspect the result to see what, if anything, remains.
func Seal(data []byte) ([]byte, error) {
	h, err := lookup(data)
	if err != nil || h.seal == nil {
		return nil, ErrUnsupported
	}
	return h.seal(data)
}

// Verdict sums up a file's findings.
type Verdict string

const (
	Silent   Verdict = "silent"
	Whispers Verdict = "whispers"
	Betrays  Verdict = "betrays"
)

func VerdictOf(fs []Finding) Verdict {
	v := Silent
	for _, f := range fs {
		if f.Severity >= Medium {
			return Betrays
		}
		v = Whispers
	}
	return v
}
