package jpegfile

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"regexp"
	"strings"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/xmp"
)

const (
	exifPrefix = "Exif\x00\x00"
	xmpPrefix  = "http://ns.adobe.com/xap/1.0/\x00"
	psPrefix   = "Photoshop 3.0\x00"
)

type metadata struct {
	exif   *exif.Meta
	xmp    xmp.Packet
	iptc   map[byte][]string
	notes  []string
	coders []string
}

func read(f *file) metadata {
	md := metadata{xmp: xmp.Packet{}, iptc: map[byte][]string{}}
	for _, s := range f.segments {
		if d, ok := s.app(exifPrefix); ok && s.Marker == 0xE1 && md.exif == nil {
			md.exif, _ = exif.Parse(d)
		}
		if d, ok := s.app(xmpPrefix); ok && s.Marker == 0xE1 {
			md.xmp = xmp.Parse(d)
		}
		if d, ok := s.app(psPrefix); ok && s.Marker == 0xED {
			md.iptc = iptc(d)
		}
		if s.Marker == 0xFE {
			c := strings.TrimSpace(strings.ToValidUTF8(string(bytes.TrimRight(s.Data, "\x00")), ""))
			switch {
			case c == "":
			case encoderComment.MatchString(c):
				md.coders = append(md.coders, c)
			default:
				md.notes = append(md.notes, c)
			}
		}
	}
	return md
}

// encoderComment matches the comments JPEG libraries write about themselves,
// which say nothing about the person.
var encoderComment = regexp.MustCompile(`(?i)^(CREATOR: gd-jpeg|Created with GIMP|LEAD Technologies|Optimized by JPEGmini|AppleMark|Intel\(R\) JPEG|File source:|Compressed by jpeg-recompress|Lavc|Handmade Software|.*\bquality\s*=\s*\d+)`)

func Inspect(b []byte) ([]leak.Finding, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	md := read(f)
	var out []leak.Finding
	out = append(out, hiddenAfterEnd(f, b)...)
	if md.exif != nil && len(md.exif.Thumbnail) > 0 {
		if v, ok := compareThumb(b[:f.end], md.exif.Thumbnail); ok && v != sameImage {
			msg := "The small preview stored inside the photo shows a different picture: it was not updated when the photo was edited."
			if v == cropped {
				msg = "The photo was cropped, but its small preview still shows the whole original scene, including what was cut away."
			}
			out = append(out, leak.ThumbnailMismatch.Find(msg, fmt.Sprintf("%d-byte preview", len(md.exif.Thumbnail))).At("EXIF thumbnail"))
		}
	}
	out = append(out, iptcFindings(md.iptc)...)
	if len(md.notes) > 0 {
		out = append(out, leak.EmbeddedText.Find("The photo carries a written comment.", strings.Join(md.notes, "; ")).At("COM segment"))
	}
	findings := photo.Findings(md.exif, md.xmp)
	if len(md.coders) > 0 {
		findings = append(findings, leak.SoftwareTrail.Find("The image names the program that encoded it.", strings.Join(md.coders, "; ")).At("COM segment"))
	}
	return append(out, findings...), nil
}

func iptcFindings(rec map[byte][]string) []leak.Finding {
	var out []leak.Finding
	if names := append(rec[iptcByline], rec[iptcWriter]...); len(names) > 0 {
		out = append(out, leak.PersonalNames.Find("The photo names its photographer or caption writer.", strings.Join(names, "; ")).At("IPTC"))
	}
	var place []string
	for _, ds := range []byte{iptcSublocal, iptcCity, iptcState, iptcCountry} {
		place = append(place, rec[ds]...)
	}
	if len(place) > 0 {
		out = append(out, leak.GPSLocation.Find("The photo is labelled with a place name.", strings.Join(place, ", ")).Lower(leak.Medium).At("IPTC"))
	}
	if c := rec[iptcCaption]; len(c) > 0 {
		out = append(out, leak.EmbeddedText.Find("The photo carries a caption.", photo.Truncate(strings.Join(c, " "), 200)).At("IPTC"))
	}
	return out
}

// hiddenAfterEnd classifies what follows the end-of-image marker. Phones
// legitimately append HDR gain maps and depth maps there, which show the
// same scene; a video or an unrelated picture is another matter.
func hiddenAfterEnd(f *file, b []byte) []leak.Finding {
	t := f.trailing
	if len(bytes.Trim(t, "\x00\xff")) == 0 {
		return nil
	}
	loc := fmt.Sprintf("byte %d, after the end of the image", f.end)
	if v, ok := findVideo(t); ok {
		return []leak.Finding{leak.HiddenVideo.Find(
			fmt.Sprintf("This is a motion photo: %s recorded around the moment of the shot is stored inside it.", v),
			fmt.Sprintf("MP4 at byte %d", f.end+v.Offset),
		).At(loc)}
	}
	if bytes.HasPrefix(t, []byte{0xFF, 0xD8, 0xFF}) {
		v, ok := compareThumb(b[:f.end], t)
		switch {
		case !ok || v == sameImage:
			return nil
		case v == cropped:
			return []leak.Finding{leak.ThumbnailMismatch.Find("A second, uncropped version of the photo is stored after the visible one.", fmt.Sprintf("%d-byte JPEG", len(t))).At(loc)}
		default:
			return []leak.Finding{leak.TrailingData.Find("A second, different picture is stored after the visible one.", fmt.Sprintf("%d-byte JPEG", len(t))).Escalate(leak.High).At(loc)}
		}
	}
	return []leak.Finding{leak.TrailingData.Find(
		fmt.Sprintf("%d bytes are stored after the end of the image (%s).", len(t), sniff.Describe(t)),
		sniff.Preview(t),
	).At(loc)}
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	var out []leak.Artifact
	if md := read(f); md.exif != nil && len(md.exif.Thumbnail) > 0 {
		out = append(out, leak.Artifact{Name: "thumbnail.jpg", Note: "the preview stored in the EXIF block", Data: md.exif.Thumbnail})
	}
	t := f.trailing
	if len(bytes.Trim(t, "\x00\xff")) == 0 {
		return out, nil
	}
	if v, ok := findVideo(t); ok {
		out = append(out, leak.Artifact{Name: "motion.mp4", Note: v.String(), Data: t[v.Offset:]})
		t = t[:v.Offset]
	}
	if len(bytes.Trim(t, "\x00\xff")) > 0 {
		out = append(out, leak.Artifact{Name: "trailing" + sniff.Extension(t), Note: "data stored after the end of the image", Data: t})
	}
	return out, nil
}

// Seal keeps only what decoding needs: JFIF, the ICC colour profile, the
// Adobe colour-transform flag, tables, frame and scans. The orientation is
// carried over in a minimal EXIF block, or sealed portraits would appear
// sideways.
func Seal(b []byte) ([]byte, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	md := read(f)
	out := []byte{0xFF, 0xD8}
	wroteOrientation := false
	for _, s := range f.segments {
		_, isJFIF := s.app("JFIF\x00")
		_, isICC := s.app("ICC_PROFILE\x00")
		_, isAdobe := s.app("Adobe")
		keep := s.Marker < 0xE0 && s.Marker != 0xFE || isJFIF || isICC || isAdobe
		if !keep {
			continue
		}
		if !wroteOrientation && !isJFIF && md.exif != nil && md.exif.Orientation > 1 && md.exif.Orientation <= 8 {
			tiff := exif.Encode([]exif.Field{exif.Short(0x0112, uint16(md.exif.Orientation))}, nil, nil, nil)
			out = appendSegment(out, 0xE1, append([]byte(exifPrefix), tiff...))
			wroteOrientation = true
		}
		out = appendSegment(out, s.Marker, s.Data)
		out = append(out, s.Scan...)
	}
	out = append(out, 0xFF, 0xD9)
	if _, err := jpeg.DecodeConfig(bytes.NewReader(out)); err != nil {
		return nil, fmt.Errorf("sealed JPEG does not decode: %w", err)
	}
	return out, nil
}
