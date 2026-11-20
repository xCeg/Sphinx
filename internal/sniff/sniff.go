// Package sniff identifies formats from their first bytes, never from file
// names: a file called photo.jpg that is really a PNG is inspected as a PNG.
package sniff

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Format string

const (
	PNG     Format = "png"
	JPEG    Format = "jpeg"
	PDF     Format = "pdf"
	DOCX    Format = "docx"
	XLSX    Format = "xlsx"
	PPTX    Format = "pptx"
	ZIP     Format = "zip"
	MP4     Format = "mp4"
	Unknown Format = ""
)

func Detect(b []byte) Format {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return PNG
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return JPEG
	case isPDF(b):
		return PDF
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return office(b)
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return MP4
	}
	return Unknown
}

// isPDF accepts the header at the start of the file or, as readers do,
// within the first kilobyte, but only after binary junk such as a
// MacBinary or mail-transport prefix. Text before it means a source file
// or a log that merely mentions "%PDF-".
func isPDF(b []byte) bool {
	i := bytes.Index(b[:min(len(b), 1024)], []byte("%PDF-"))
	if i < 0 || i+7 > len(b) || b[i+5] < '1' || b[i+5] > '2' || b[i+6] != '.' {
		return false
	}
	if i == 0 {
		return true
	}
	for _, c := range b[:i] {
		if c < '\t' || c > '~' {
			return true
		}
	}
	return false
}

// office tells OOXML flavours apart by the part names stored in the zip's
// local headers, which every writer places near the start.
func office(b []byte) Format {
	head := b[:min(len(b), 1<<16)]
	switch {
	case bytes.Contains(head, []byte("word/")):
		return DOCX
	case bytes.Contains(head, []byte("xl/")):
		return XLSX
	case bytes.Contains(head, []byte("ppt/")):
		return PPTX
	}
	if bytes.Contains(b, []byte("word/document.xml")) {
		return DOCX
	}
	if bytes.Contains(b, []byte("xl/workbook.xml")) {
		return XLSX
	}
	if bytes.Contains(b, []byte("ppt/presentation.xml")) {
		return PPTX
	}
	return ZIP
}

var names = map[Format]string{
	PNG: "a PNG image", JPEG: "a JPEG image", PDF: "a PDF document", DOCX: "a Word document",
	XLSX: "an Excel workbook", PPTX: "a PowerPoint presentation", ZIP: "a zip archive", MP4: "a video",
}

// Describe names what b looks like, for messages about unexpected data.
func Describe(b []byte) string {
	if n, ok := names[Detect(b)]; ok {
		return "it looks like " + n
	}
	if bytes.Contains(b[:min(len(b), 4096)], []byte("ftyp")) {
		return "it contains a video"
	}
	if isText(b) {
		return "it is readable text"
	}
	if allZero(b) {
		return "it is padding made of zero bytes"
	}
	return "format not recognised"
}

func Extension(b []byte) string {
	if f := Detect(b); f != Unknown {
		return "." + string(f)
	}
	if isText(b) {
		return ".txt"
	}
	return ".bin"
}

func isText(b []byte) bool {
	s := b[:min(len(b), 4096)]
	if !utf8.Valid(s) && len(b) <= 4096 {
		return false
	}
	printable := 0
	for _, r := range string(s) {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}
	return len(s) > 0 && printable*100 >= utf8.RuneCount(s)*95
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// Preview shows the start of b as text when it is text, and in hex otherwise.
func Preview(b []byte) string {
	s := b[:min(len(b), 48)]
	if isText(b) {
		return strings.TrimSpace(string(s))
	}
	var sb strings.Builder
	for i, c := range s {
		if i > 0 {
			sb.WriteByte(' ')
		}
		const digits = "0123456789abcdef"
		sb.WriteByte(digits[c>>4])
		sb.WriteByte(digits[c&15])
	}
	if len(b) > len(s) {
		sb.WriteString(" …")
	}
	return sb.String()
}
