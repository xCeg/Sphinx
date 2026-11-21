// Package webpfile inspects, reveals and seals WebP images.
package webpfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xceg/sphinx/internal/c2pa"
	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/xmp"
)

type chunk struct {
	id   string
	data []byte
}

var errNotWebP = errors.New("not a WebP file")

// parse returns the chunks and whatever follows the RIFF container.
func parse(b []byte) ([]chunk, []byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil, nil, errNotWebP
	}
	end := 8 + int(binary.LittleEndian.Uint32(b[4:]))
	if end > len(b) || end < 12 {
		return nil, nil, fmt.Errorf("WebP container claims %d bytes, more than the file holds", end)
	}
	var out []chunk
	for p := 12; p+8 <= end; {
		n := int(binary.LittleEndian.Uint32(b[p+4:]))
		if n < 0 || p+8+n > end {
			return nil, nil, fmt.Errorf("WebP chunk at byte %d is cut off", p)
		}
		out = append(out, chunk{string(b[p : p+4]), b[p+8 : p+8+n]})
		p += 8 + n + n%2
	}
	return out, b[end:], nil
}

func Inspect(b []byte) ([]leak.Finding, error) {
	chunks, trailing, err := parse(b)
	if err != nil {
		return nil, err
	}
	var out []leak.Finding
	if len(bytes.Trim(trailing, "\x00")) > 0 {
		out = append(out, leak.TrailingData.Find(
			fmt.Sprintf("%d bytes are stored after the end of the image (%s).", len(trailing), sniff.Describe(trailing)),
			sniff.Preview(trailing)).At("after the RIFF container"))
	}
	var meta *exif.Meta
	packet := xmp.Packet{}
	for _, c := range chunks {
		switch c.id {
		case "EXIF":
			meta, _ = exif.Parse(bytes.TrimPrefix(c.data, []byte("Exif\x00\x00")))
		case "XMP ":
			packet = xmp.Parse(c.data)
		case "C2PA":
			out = append(out, credentials(c.data).At("C2PA chunk"))
		}
	}
	return append(out, photo.Findings(meta, packet)...), nil
}

func credentials(b []byte) leak.Finding {
	gen := c2pa.Generator(b)
	msg := "The image carries Content Credentials: a signed record of how it was made and edited."
	return leak.ContentCredentials.Find(msg, gen)
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	_, trailing, err := parse(b)
	if err != nil || len(bytes.Trim(trailing, "\x00")) == 0 {
		return nil, err
	}
	return []leak.Artifact{{Name: "trailing" + sniff.Extension(trailing), Note: "data stored after the end of the image", Data: trailing}}, nil
}

// keep lists the chunks that make up the picture itself.
var keep = map[string]bool{"VP8 ": true, "VP8L": true, "VP8X": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true}

// Seal drops EXIF, XMP and C2PA chunks and clears the matching VP8X flags,
// which decoders check before looking for those chunks.
func Seal(b []byte) ([]byte, error) {
	chunks, _, err := parse(b)
	if err != nil {
		return nil, err
	}
	body := []byte("WEBP")
	for _, c := range chunks {
		if !keep[c.id] {
			continue
		}
		data := c.data
		if c.id == "VP8X" && len(data) >= 1 {
			data = append([]byte{data[0] &^ (0x08 | 0x04)}, data[1:]...)
		}
		body = append(body, c.id...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(data)))
		body = append(body, data...)
		if len(data)%2 == 1 {
			body = append(body, 0)
		}
	}
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...), nil
}
