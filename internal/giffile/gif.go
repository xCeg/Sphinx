// Package giffile inspects and seals GIF images: their comment blocks,
// XMP application extensions and data after the trailer.
package giffile

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/xmp"
)

type block struct {
	start, end int
	kind       byte   // 0x21 extension, 0x2C image
	label      byte   // extension label
	app        string // application identifier
	data       []byte // concatenated sub-blocks
}

var errNotGIF = errors.New("not a GIF file")

// parse walks the block structure of a GIF. It returns the blocks, the
// offset just past the trailer and what follows it.
func parse(b []byte) ([]block, int, error) {
	if len(b) < 13 || (!bytes.HasPrefix(b, []byte("GIF87a")) && !bytes.HasPrefix(b, []byte("GIF89a"))) {
		return nil, 0, errNotGIF
	}
	p := 13
	if b[10]&0x80 != 0 {
		p += 3 << (int(b[10]&7) + 1)
	}
	var out []block
	subBlocks := func(p int) ([]byte, int, error) {
		var data []byte
		for {
			if p >= len(b) {
				return nil, p, errors.New("GIF ends inside a block")
			}
			n := int(b[p])
			p++
			if n == 0 {
				return data, p, nil
			}
			if p+n > len(b) {
				return nil, p, errors.New("GIF ends inside a block")
			}
			data = append(data, b[p:p+n]...)
			p += n
		}
	}
	for p < len(b) {
		start := p
		switch b[p] {
		case 0x3B:
			return out, p + 1, nil
		case 0x21:
			if p+2 > len(b) {
				return nil, 0, errors.New("GIF ends inside an extension")
			}
			blk := block{start: start, kind: 0x21, label: b[p+1]}
			data, end, err := subBlocks(p + 2)
			if err != nil {
				return nil, 0, err
			}
			if blk.label == 0xFF && len(data) >= 11 {
				blk.app = string(data[:11])
				blk.data = data[11:]
			} else {
				blk.data = data
			}
			blk.end = end
			out = append(out, blk)
			p = end
		case 0x2C:
			if p+11 > len(b) {
				return nil, 0, errors.New("GIF ends inside an image")
			}
			q := p + 10
			if b[p+9]&0x80 != 0 {
				q += 3 << (int(b[p+9]&7) + 1)
			}
			_, end, err := subBlocks(q + 1)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, block{start: start, end: end, kind: 0x2C})
			p = end
		default:
			return nil, 0, fmt.Errorf("unexpected GIF block 0x%02x at byte %d", b[p], p)
		}
	}
	return out, len(b), nil
}

// The XMP application extension stores the packet raw, followed by a
// 257-byte "magic trailer" that makes sub-block parsing land on it.
func xmpData(blk block) []byte {
	if blk.app != "XMP DataXMP" {
		return nil
	}
	return blk.data
}

func Inspect(b []byte) ([]leak.Finding, error) {
	blocks, end, err := parse(b)
	if err != nil {
		return nil, err
	}
	var out []leak.Finding
	if t := b[end:]; len(bytes.Trim(t, "\x00")) > 0 {
		out = append(out, leak.TrailingData.Find(
			fmt.Sprintf("%d bytes are stored after the end of the image (%s).", len(t), sniff.Describe(t)),
			sniff.Preview(t)).At(fmt.Sprintf("byte %d", end)))
	}
	var notes []string
	packet := xmp.Packet{}
	for _, blk := range blocks {
		switch {
		case blk.kind == 0x21 && blk.label == 0xFE:
			if c := strings.TrimSpace(string(blk.data)); c != "" {
				notes = append(notes, photo.Truncate(c, 200))
			}
		case xmpData(blk) != nil:
			packet = xmp.Parse(xmpData(blk))
		}
	}
	if len(notes) > 0 {
		out = append(out, leak.EmbeddedText.Find("The image carries a written comment.", strings.Join(notes, "; ")).At("comment extension"))
	}
	return append(out, photo.Findings(nil, packet)...), nil
}

// Seal drops comment and application extensions other than the animation
// loop count, and anything after the trailer.
func Seal(b []byte) ([]byte, error) {
	blocks, end, err := parse(b)
	if err != nil {
		return nil, err
	}
	first := len(b[:end])
	if len(blocks) > 0 {
		first = blocks[0].start
	}
	out := append([]byte(nil), b[:first]...)
	for _, blk := range blocks {
		drop := blk.kind == 0x21 && (blk.label == 0xFE || (blk.label == 0xFF && !strings.HasPrefix(blk.app, "NETSCAPE") && !strings.HasPrefix(blk.app, "ANIMEXTS")))
		if !drop {
			out = append(out, b[blk.start:blk.end]...)
		}
	}
	return append(out, 0x3B), nil
}
