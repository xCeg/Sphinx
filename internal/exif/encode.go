package exif

import (
	"encoding/binary"
	"sort"
)

// Field is one IFD entry to encode.
type Field struct {
	Tag   uint16
	Type  uint16
	Count uint32
	Value []byte
}

func ASCII(t uint16, s string) Field {
	v := append([]byte(s), 0)
	return Field{t, 2, uint32(len(v)), v}
}

func Short(t uint16, v uint16) Field {
	return Field{t, 3, 1, binary.LittleEndian.AppendUint16(nil, v)}
}

func Rationals(t uint16, pairs ...[2]uint32) Field {
	var b []byte
	for _, p := range pairs {
		b = binary.LittleEndian.AppendUint32(b, p[0])
		b = binary.LittleEndian.AppendUint32(b, p[1])
	}
	return Field{t, 5, uint32(len(pairs)), b}
}

func Bytes(t uint16, v []byte) Field { return Field{t, 1, uint32(len(v)), v} }

// Encode writes a little-endian TIFF structure. Sub-IFDs and the thumbnail
// are linked from IFD0 and IFD1 automatically when non-empty.
func Encode(ifd0, exifIFD, gpsIFD []Field, thumb []byte) []byte {
	type block struct {
		fields []Field
		link   *uint32
	}
	var exifOff, gpsOff, ifd1Off uint32
	main := append([]Field(nil), ifd0...)
	if len(exifIFD) > 0 {
		main = append(main, Field{uint16(tagExifIFD), 4, 1, make([]byte, 4)})
	}
	if len(gpsIFD) > 0 {
		main = append(main, Field{uint16(tagGPSIFD), 4, 1, make([]byte, 4)})
	}
	var ifd1 []Field
	if len(thumb) > 0 {
		ifd1 = []Field{
			{uint16(tagThumbOffset), 4, 1, make([]byte, 4)},
			{uint16(tagThumbLength), 4, 1, binary.LittleEndian.AppendUint32(nil, uint32(len(thumb)))},
		}
	}

	size := func(fs []Field) uint32 {
		n := uint32(2 + len(fs)*12 + 4)
		for _, f := range fs {
			if len(f.Value) > 4 {
				n += uint32(len(f.Value)+1) &^ 1
			}
		}
		return n
	}
	off := uint32(8) + size(main)
	if len(exifIFD) > 0 {
		exifOff, off = off, off+size(exifIFD)
	}
	if len(gpsIFD) > 0 {
		gpsOff, off = off, off+size(gpsIFD)
	}
	if len(ifd1) > 0 {
		ifd1Off, off = off, off+size(ifd1)
		binary.LittleEndian.PutUint32(ifd1[0].Value, off)
	}
	for i := range main {
		switch tag(main[i].Tag) {
		case tagExifIFD:
			binary.LittleEndian.PutUint32(main[i].Value, exifOff)
		case tagGPSIFD:
			binary.LittleEndian.PutUint32(main[i].Value, gpsOff)
		}
	}

	out := []byte{'I', 'I', 42, 0, 8, 0, 0, 0}
	write := func(fs []Field, next uint32) {
		sort.Slice(fs, func(i, j int) bool { return fs[i].Tag < fs[j].Tag })
		start := uint32(len(out))
		data := start + uint32(2+len(fs)*12+4)
		out = binary.LittleEndian.AppendUint16(out, uint16(len(fs)))
		var extra []byte
		for _, f := range fs {
			out = binary.LittleEndian.AppendUint16(out, f.Tag)
			out = binary.LittleEndian.AppendUint16(out, f.Type)
			out = binary.LittleEndian.AppendUint32(out, f.Count)
			if len(f.Value) <= 4 {
				v := make([]byte, 4)
				copy(v, f.Value)
				out = append(out, v...)
				continue
			}
			out = binary.LittleEndian.AppendUint32(out, data+uint32(len(extra)))
			extra = append(extra, f.Value...)
			if len(extra)%2 == 1 {
				extra = append(extra, 0)
			}
		}
		out = binary.LittleEndian.AppendUint32(out, next)
		out = append(out, extra...)
	}
	write(main, ifd1Off)
	if len(exifIFD) > 0 {
		write(append([]Field(nil), exifIFD...), 0)
	}
	if len(gpsIFD) > 0 {
		write(append([]Field(nil), gpsIFD...), 0)
	}
	if len(ifd1) > 0 {
		write(ifd1, 0)
	}
	return append(out, thumb...)
}
