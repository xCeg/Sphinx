package exif

import "encoding/binary"

// Tags of a TIFF structure that hold embedded metadata blocks rather than
// image parameters: XMP, IPTC and Photoshop resources.
const (
	tagXMP       tag = 0x02BC
	tagIPTC      tag = 0x83BB
	tagPhotoshop tag = 0x8649
)

var personal = []tag{
	tagDescription, tagMake, tagModel, tagSoftware, tagDateTime, tagArtist, tagHostComputer,
	tagCopyright, tagDTOriginal, tagDTDigitized, tagOffsetTime, tagUserComment, tagXPComment,
	tagXPAuthor, tagXPSubject, tagUniqueID, tagOwner, tagBodySerial, tagLensModel, tagLensSerial,
	tagXMP, tagIPTC, tagPhotoshop,
}

// Blank zeroes, in place, the values of identifying tags and every value
// of the GPS directory. The structure, offsets and image data are left as
// they are, so the file stays valid for any reader: this is how TIFF and
// DNG files, whose pixel data hangs off the same directories, are sealed.
func Blank(b []byte) error {
	if _, err := Parse(b); err != nil {
		return err
	}
	r := &reader{b: b, visited: map[uint32]bool{}}
	r.order = binary.BigEndian
	if string(b[:2]) == "II" {
		r.order = binary.LittleEndian
	}
	for off, depth := r.order.Uint32(b[4:]), 0; off != 0 && depth < 64; depth++ {
		ifd, next := r.ifd(off)
		blankEntries(ifd, personal)
		if o, ok := r.long(ifd[tagExifIFD]); ok {
			sub, _ := r.ifd(o)
			blankEntries(sub, personal)
		}
		if o, ok := r.long(ifd[tagGPSIFD]); ok {
			gps, _ := r.ifd(o)
			for _, e := range gps {
				blankValue(e)
			}
		}
		off = next
	}
	return nil
}

func blankEntries(ifd map[tag]entry, tags []tag) {
	for _, t := range tags {
		if e, ok := ifd[t]; ok {
			blankValue(e)
		}
	}
}

// blankValue zeroes a value. ASCII values become spaces ending in a NUL,
// because libtiff warns about strings made only of NULs.
func blankValue(e entry) {
	if e.typ == 2 && len(e.value) > 0 {
		for i := range e.value {
			e.value[i] = ' '
		}
		e.value[len(e.value)-1] = 0
		return
	}
	clear(e.value)
}
