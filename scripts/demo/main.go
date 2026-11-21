// Command demo builds the example files used on the website and in the
// README from the renderings made by render.js:
//
//	node scripts/demo/render.js /tmp/demo
//	go run ./scripts/demo /tmp/demo site/examples
//
// Every leak in them is planted on purpose and every name is invented.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"github.com/xceg/sphinx/internal/exif"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: demo RENDERDIR OUTDIR")
		os.Exit(2)
	}
	in, out := os.Args[1], os.Args[2]
	steps := []func(string, string) error{screenshot, holiday, memo, offer, cv, logo, folder}
	for _, step := range steps {
		if err := step(in, out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func decodePNG(path string) (*image.NRGBA, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Bounds(), img, image.Point{}, draw.Src)
	return n, raw, nil
}

// screenshot reproduces aCropalypse: the cropped picture is written over
// the original file without truncating it.
func screenshot(in, out string) error {
	img, full, err := decodePNG(filepath.Join(in, "screenshot.png"))
	if err != nil {
		return err
	}
	var small bytes.Buffer
	if err := png.Encode(&small, img.SubImage(image.Rect(0, 0, 1080, 620))); err != nil {
		return err
	}
	file := append(small.Bytes(), full[small.Len():]...)
	return os.WriteFile(filepath.Join(out, "cropped-screenshot.png"), file, 0o644)
}

// holiday is a photo cropped to the sunset, whose preview still shows the
// house and its address, and whose EXIF still holds the GPS position.
func holiday(in, out string) error {
	img, _, err := decodePNG(filepath.Join(in, "photo.png"))
	if err != nil {
		return err
	}
	var thumb, main bytes.Buffer
	if err := jpeg.Encode(&thumb, scale(img, 160, 107), &jpeg.Options{Quality: 80}); err != nil {
		return err
	}
	if err := jpeg.Encode(&main, img.SubImage(image.Rect(800, 0, 1600, 600)), &jpeg.Options{Quality: 88}); err != nil {
		return err
	}
	tiff := exif.Encode(
		[]exif.Field{exif.ASCII(0x010F, "Google"), exif.ASCII(0x0110, "Pixel 7"), exif.ASCII(0x0131, "HDR+ 1.0.540104767zd")},
		[]exif.Field{exif.ASCII(0x9003, "2024:08:17 20:41:09"), exif.ASCII(0x9010, "+02:00"), exif.ASCII(0xA431, "28041JEGR10466")},
		[]exif.Field{
			exif.ASCII(1, "N"), exif.Rationals(2, [2]uint32{45, 1}, [2]uint32{53, 1}, [2]uint32{5712, 100}),
			exif.ASCII(3, "E"), exif.Rationals(4, [2]uint32{6, 1}, [2]uint32{7, 1}, [2]uint32{4584, 100}),
		},
		thumb.Bytes(),
	)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(app1)+2))
	file := append([]byte{0xFF, 0xD8}, append(append(seg, app1...), main.Bytes()[2:]...)...)
	return os.WriteFile(filepath.Join(out, "holiday.jpg"), file, 0o644)
}

func scale(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	for y := range h {
		for x := range w {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	return dst
}

func memo(in, out string) error {
	b, err := os.ReadFile(filepath.Join(in, "memo.pdf"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "redacted-memo.pdf"), b, 0o644)
}

// offer is a Word file sent "clean" with Track Changes still holding the
// negotiation floor, a reviewer's comment and the author's template path.
func offer(_, out string) error {
	const w = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/><Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document ` + w + `><w:body>
<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Offer of employment</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Dear Ms Varga, we are pleased to offer you the position of Lead Designer at a gross annual salary of </w:t></w:r><w:del w:id="1" w:author="Tom Becker" w:date="2024-05-02T09:14:00Z"><w:r><w:delText>€58,000. Our ceiling for this role is €72,000.</w:delText></w:r></w:del><w:ins w:id="2" w:author="Tom Becker" w:date="2024-05-02T09:14:00Z"><w:r><w:t>€58,000.</w:t></w:r></w:ins></w:p>
<w:p><w:commentRangeStart w:id="0"/><w:r><w:t>Please reply by 31 May.</w:t></w:r><w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r></w:p>
<w:sectPr/></w:body></w:document>`},
		{"word/comments.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:comments ` + w + `><w:comment w:id="0" w:author="Ines Morel" w:date="2024-05-02T10:02:00Z"><w:p><w:r><w:t>She also interviewed at Halden Studio; push for a quick answer.</w:t></w:r></w:p></w:comment></w:comments>`},
		{"word/settings.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings ` + w + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:attachedTemplate r:id="rId1"/></w:settings>`},
		{"word/_rels/settings.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate" Target="file:///C:\Users\tbecker\Documents\HR\Templates\Offer%20letter.dotx" TargetMode="External"/></Relationships>`},
		{"docProps/core.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>Offer of employment</dc:title><dc:creator>Tom Becker</dc:creator><cp:lastModifiedBy>Ines Morel</cp:lastModifiedBy><cp:revision>7</cp:revision><dcterms:created xsi:type="dcterms:W3CDTF">2024-05-01T16:20:00Z</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">2024-05-02T10:05:00Z</dcterms:modified></cp:coreProperties>`},
		{"docProps/app.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Template>Offer letter.dotx</Template><TotalTime>94</TotalTime><Application>Microsoft Office Word</Application><Company>Lantern Works</Company></Properties>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		f, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		f.Write([]byte(p.body))
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "offer-letter.docx"), buf.Bytes(), 0o644)
}

// cv is a résumé with an instruction to AI screening tools in white text
// and a list of keywords in a one-pixel font.
func cv(in, out string) error {
	b, err := os.ReadFile(filepath.Join(in, "cv.pdf"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "cv.pdf"), b, 0o644)
}

// logo is an SVG as Inkscape saves it, with a hidden layer holding an
// earlier proposal for another client and the designer's export path.
func logo(_, out string) error {
	svg := `<?xml version="1.0" encoding="UTF-8"?>
<!-- Created with Inkscape (http://www.inkscape.org/) -->
<svg xmlns="http://www.w3.org/2000/svg" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape"
   xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd" width="320" height="120" viewBox="0 0 320 120"
   inkscape:version="1.3.2 (091e20e, 2023-11-25)" sodipodi:docname="lantern-logo-FINAL-v7.svg"
   inkscape:export-filename="/Users/tbecker/Clients/Lantern Works/2024-rebrand/logo@2x.png">
  <sodipodi:namedview id="namedview1" pagecolor="#ffffff" inkscape:zoom="2.4"/>
  <g inkscape:groupmode="layer" inkscape:label="Logo">
    <circle cx="60" cy="60" r="40" fill="#c9a04e"/>
    <text x="120" y="72" font-family="Georgia" font-size="34" fill="#1d2740">Lantern</text>
  </g>
  <g inkscape:groupmode="layer" inkscape:label="Old pitch" style="display:none">
    <text x="120" y="100" font-size="12">Same concept pitched to Halden Studio in March, rejected at 4,500 EUR</text>
  </g>
</svg>
`
	return os.WriteFile(filepath.Join(out, "logo.svg"), []byte(svg), 0o644)
}

// folder is a zip made from a Mac folder: it carries the folder's
// .DS_Store, which names two files that were left out of the archive.
func folder(_, out string) error {
	ds, err := os.ReadFile("internal/zipfile/testdata/DS_Store")
	if err != nil {
		return err
	}
	offer, err := os.ReadFile(filepath.Join(out, "offer-letter.docx"))
	if err != nil {
		return err
	}
	photo, err := os.ReadFile(filepath.Join(out, "holiday.jpg"))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"Shared/offer-letter.docx", offer},
		{"Shared/holiday.jpg", photo},
		{"Shared/.DS_Store", ds},
		{"__MACOSX/Shared/._holiday.jpg", []byte("\x00\x05\x16\x07\x00\x02\x00\x00Mac OS X        ")},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		w.Write(f.data)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "shared-folder.zip"), buf.Bytes(), 0o644)
}
