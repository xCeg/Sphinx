package pngfile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image/png"
	"io"
	"strings"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/sniff"
	"github.com/xceg/sphinx/internal/xmp"
)

func Inspect(b []byte) ([]leak.Finding, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	var out []leak.Finding
	if len(f.trailing) > 0 {
		out = append(out, trailingFinding(f))
	}
	var meta *exif.Meta
	packet := xmp.Packet{}
	for _, c := range f.chunks {
		switch c.Type {
		case "eXIf":
			if m, err := exif.Parse(c.Data); err == nil {
				meta = m
			}
		case "tIME":
			if len(c.Data) == 7 {
				d := c.Data
				when := fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d UTC", binary.BigEndian.Uint16(d), d[2], d[3], d[4], d[5], d[6])
				out = append(out, leak.Timestamps.Find("The image records when it was last modified.", when).At("tIME chunk"))
			}
		case "tEXt", "zTXt", "iTXt":
			key, text, ok := textChunk(c)
			if !ok {
				continue
			}
			if m, isExif := rawProfileExif(key, text); isExif {
				meta = m
				continue
			}
			if key == "XML:com.adobe.xmp" {
				packet = xmp.Parse([]byte(text))
				continue
			}
			if fd, ok := textFinding(key, text); ok {
				out = append(out, fd.At(c.Type+" chunk \""+key+"\""))
			}
		}
	}
	return append(out, photo.Findings(meta, packet)...), nil
}

func trailingFinding(f *file) leak.Finding {
	n := len(f.trailing)
	if r, ok := recoverCrop(f.header, f.trailing); ok {
		return leak.HiddenCrop.Find(
			fmt.Sprintf("The picture was cropped, but %d rows of the original %d-pixel-wide image are still stored after it. `sphinx reveal` turns them back into an image.", r.Rows, r.Width),
			fmt.Sprintf("%s hidden after the end of the visible image", kilobytes(n)),
		).At(fmt.Sprintf("byte %d", f.end))
	}
	kind := sniff.Describe(f.trailing)
	return leak.TrailingData.Find(
		fmt.Sprintf("%d bytes are stored after the end of the image (%s).", n, kind),
		sniff.Preview(f.trailing),
	).At(fmt.Sprintf("byte %d", f.end))
}

// textChunk decodes tEXt, zTXt and iTXt. Latin-1 is widened to UTF-8.
func textChunk(c chunk) (key, text string, ok bool) {
	i := bytes.IndexByte(c.Data, 0)
	if i <= 0 {
		return "", "", false
	}
	key, rest := string(c.Data[:i]), c.Data[i+1:]
	switch c.Type {
	case "tEXt":
		return key, latin1(rest), true
	case "zTXt":
		if len(rest) < 1 {
			return "", "", false
		}
		t, err := inflateText(rest[1:])
		return key, latin1(t), err == nil
	}
	if len(rest) < 2 {
		return "", "", false
	}
	compressed, body := rest[0] == 1, rest[2:]
	for range 2 {
		j := bytes.IndexByte(body, 0)
		if j < 0 {
			return "", "", false
		}
		body = body[j+1:]
	}
	if compressed {
		t, err := inflateText(body)
		return key, string(t), err == nil
	}
	return key, string(body), true
}

func inflateText(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(r, 16<<20))
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// rawProfileExif reads the hex-encoded EXIF that ImageMagick stores in a
// "Raw profile type exif" text chunk: a newline, a type name, a length,
// then hex digits.
func rawProfileExif(key, text string) (*exif.Meta, bool) {
	if key != "Raw profile type exif" && key != "Raw profile type APP1" {
		return nil, false
	}
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return nil, true
	}
	raw, err := hex.DecodeString(strings.Join(fields[2:], ""))
	if err != nil {
		return nil, true
	}
	raw = bytes.TrimPrefix(raw, []byte("Exif\x00\x00"))
	m, err := exif.Parse(raw)
	if err != nil {
		return nil, true
	}
	return m, true
}

var aiKeys = map[string]bool{
	"parameters": true, "prompt": true, "workflow": true, "Dream": true,
	"sd-metadata": true, "invokeai_metadata": true, "generation_data": true,
}

func textFinding(key, text string) (leak.Finding, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return leak.Finding{}, false
	}
	short := photo.Truncate(text, 200)
	switch {
	case aiKeys[key]:
		return leak.EmbeddedText.Find("The image stores the full prompt and settings used to generate it with AI.", short).Escalate(leak.Medium), true
	case key == "Author":
		return leak.PersonalNames.Find("The image names its author.", short), true
	case key == "Software":
		return leak.SoftwareTrail.Find("The image names the software that produced it.", short), true
	case key == "Creation Time":
		return leak.Timestamps.Find("The image records when it was created.", short), true
	case key == "date:create" || key == "date:modify" || key == "date:timestamp":
		return leak.Timestamps.Find("The image records when it was processed.", short), true
	}
	return leak.EmbeddedText.Find(fmt.Sprintf("The image carries a text note named %q.", key), short), true
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	if len(f.trailing) == 0 {
		return nil, nil
	}
	if r, ok := recoverCrop(f.header, f.trailing); ok {
		var buf bytes.Buffer
		if err := png.Encode(&buf, r.Image); err != nil {
			return nil, err
		}
		return []leak.Artifact{{
			Name: "uncropped.png",
			Note: fmt.Sprintf("bottom %d rows of the original %d px wide picture", r.Rows, r.Width),
			Data: buf.Bytes(),
		}}, nil
	}
	return []leak.Artifact{{Name: "trailing" + sniff.Extension(f.trailing), Note: "data stored after the end of the image", Data: f.trailing}}, nil
}

// keep lists the ancillary chunks that affect how pixels look. Everything
// else that is ancillary (lowercase first letter) is metadata and goes.
var keep = map[string]bool{
	"tRNS": true, "gAMA": true, "cHRM": true, "sRGB": true, "iCCP": true, "sBIT": true,
	"pHYs": true, "cICP": true, "mDCv": true, "cLLI": true, "acTL": true, "fcTL": true, "fdAT": true,
}

func Seal(b []byte) ([]byte, error) {
	f, err := parse(b)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), signature...)
	for _, c := range f.chunks {
		critical := c.Type[0] >= 'A' && c.Type[0] <= 'Z'
		if critical || keep[c.Type] {
			out = appendChunk(out, c.Type, c.Data)
		}
	}
	return out, nil
}

func kilobytes(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}
