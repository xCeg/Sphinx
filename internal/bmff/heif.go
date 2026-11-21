package bmff

import (
	"bytes"
	"encoding/binary"
)

// item is an entry of a HEIF meta box with the byte ranges it occupies.
type item struct {
	id          uint32
	typ, mime   string
	extents     [][2]int
	constructed bool
}

// heifItems reads iinf and iloc from the top-level meta box. Items stored
// in idat (construction method 1) are resolved against it.
func heifItems(f []byte) []item {
	var meta *box
	walk(f, 0, len(f), nil, 0, func(b *box) {
		if b.typ == "meta" && b.parent == nil {
			meta = b
		}
	})
	if meta == nil {
		return nil
	}
	items := map[uint32]*item{}
	var order []uint32
	var idat *box
	walk(f, meta.body+4, meta.end, meta, 1, func(b *box) {
		switch b.typ {
		case "infe":
			if it := parseInfe(b.data(f)); it != nil {
				items[it.id] = it
				order = append(order, it.id)
			}
		case "idat":
			idat = b
		}
	})
	var iloc *box
	walk(f, meta.body+4, meta.end, meta, 1, func(b *box) {
		if b.typ == "iinf" {
			d := b.data(f)
			if len(d) < 6 {
				return
			}
			skip := 6
			if d[0] != 0 {
				skip = 8
			}
			walk(f, b.body+skip, b.end, b, 2, func(c *box) {
				if c.typ == "infe" {
					if it := parseInfe(c.data(f)); it != nil && items[it.id] == nil {
						items[it.id] = it
						order = append(order, it.id)
					}
				}
			})
		}
		if b.typ == "iloc" {
			iloc = b
		}
	})
	// iloc usually precedes iinf, so locations are read once items are known.
	if iloc != nil {
		parseIloc(iloc.data(f), items, idat)
	}
	// Extents come straight from the file; ranges outside it are dropped
	// before anything reads or blanks them.
	var out []item
	for _, id := range order {
		it := *items[id]
		var ok [][2]int
		for _, e := range it.extents {
			if e[0] >= 0 && e[0] <= e[1] && e[1] <= len(f) {
				ok = append(ok, e)
			}
		}
		it.extents = ok
		out = append(out, it)
	}
	return out
}

func parseInfe(d []byte) *item {
	if len(d) < 4 || d[0] < 2 {
		return nil
	}
	p := 4
	var id uint32
	if d[0] == 2 {
		if len(d) < p+8 {
			return nil
		}
		id = uint32(binary.BigEndian.Uint16(d[p:]))
		p += 2
	} else {
		if len(d) < p+10 {
			return nil
		}
		id = binary.BigEndian.Uint32(d[p:])
		p += 4
	}
	p += 2
	it := &item{id: id, typ: string(d[p : p+4])}
	p += 4
	if i := bytes.IndexByte(d[p:], 0); i >= 0 {
		p += i + 1
		if it.typ == "mime" {
			if j := bytes.IndexByte(d[p:], 0); j >= 0 {
				it.mime = string(d[p : p+j])
			}
		}
	}
	return it
}

func parseIloc(d []byte, items map[uint32]*item, idat *box) {
	if len(d) < 8 {
		return
	}
	version := d[0]
	offSize, lenSize := int(d[4]>>4), int(d[4]&15)
	baseSize, idxSize := int(d[5]>>4), 0
	if version == 1 || version == 2 {
		idxSize = int(d[5] & 15)
	}
	p := 6
	read := func(n int) (uint64, bool) {
		if n == 0 {
			return 0, true
		}
		if p+n > len(d) || n > 8 {
			return 0, false
		}
		var v uint64
		for _, c := range d[p : p+n] {
			v = v<<8 | uint64(c)
		}
		p += n
		return v, true
	}
	countSize := 2
	if version == 2 {
		countSize = 4
	}
	count, ok := read(countSize)
	if !ok {
		return
	}
	for i := uint64(0); i < count && i < 1<<16; i++ {
		idSize := 2
		if version == 2 {
			idSize = 4
		}
		id, ok := read(idSize)
		if !ok {
			return
		}
		method := uint64(0)
		if version == 1 || version == 2 {
			m, ok := read(2)
			if !ok {
				return
			}
			method = m & 15
		}
		if _, ok := read(2); !ok {
			return
		}
		base, ok1 := read(baseSize)
		n, ok2 := read(2)
		if !ok1 || !ok2 {
			return
		}
		it := items[uint32(id)]
		for e := uint64(0); e < n; e++ {
			if _, ok := read(idxSize); !ok {
				return
			}
			off, ok1 := read(offSize)
			length, ok2 := read(lenSize)
			if !ok1 || !ok2 {
				return
			}
			if it == nil {
				continue
			}
			start := int(base + off)
			if method == 1 {
				if idat == nil {
					continue
				}
				start += idat.body
				it.constructed = true
			}
			it.extents = append(it.extents, [2]int{start, start + int(length)})
		}
	}
}

func (it item) bytes(f []byte) []byte {
	var out []byte
	for _, e := range it.extents {
		if e[0] >= 0 && e[1] <= len(f) && e[0] <= e[1] {
			out = append(out, f[e[0]:e[1]]...)
		}
	}
	return out
}

// exifPayload skips the 4-byte offset that precedes the TIFF header in a
// HEIF Exif item, and the "Exif\0\0" marker some writers keep.
func exifPayload(b []byte) []byte {
	if len(b) < 4 {
		return nil
	}
	skip := int(binary.BigEndian.Uint32(b))
	if 4+skip > len(b) {
		return nil
	}
	return bytes.TrimPrefix(b[4+skip:], []byte("Exif\x00\x00"))
}
