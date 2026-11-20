package ooxml

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
)

const (
	wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	rNS = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
)

func build(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	if _, ok := parts["[Content_Types].xml"]; !ok {
		parts["[Content_Types].xml"] = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`
	}
	for _, name := range []string{"[Content_Types].xml"} {
		f, _ := w.Create(name)
		f.Write([]byte(parts[name]))
	}
	for name, body := range parts {
		if name == "[Content_Types].xml" {
			continue
		}
		f, _ := w.Create(name)
		f.Write([]byte(body))
	}
	w.Close()
	return b.Bytes()
}

func docx(t *testing.T, body string, extra map[string]string) []byte {
	parts := map[string]string{
		"word/document.xml": `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`,
	}
	for k, v := range extra {
		parts[k] = v
	}
	return build(t, parts)
}

func byRule(fs []leak.Finding) map[string]leak.Finding {
	m := map[string]leak.Finding{}
	for _, f := range fs {
		m[f.Rule] = f
	}
	return m
}

func inspect(t *testing.T, b []byte) map[string]leak.Finding {
	t.Helper()
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	return byRule(fs)
}

func TestTrackedDeletion(t *testing.T) {
	b := docx(t, `<w:p><w:r><w:t>The price is </w:t></w:r>
		<w:del w:id="1" w:author="Jane Roe"><w:r><w:delText>€2.1M, our floor is €1.6M</w:delText></w:r></w:del>
		<w:ins w:id="2" w:author="Jane Roe"><w:r><w:t>negotiable</w:t></w:r></w:ins></w:p>`, nil)
	f, ok := inspect(t, b)[leak.DeletedText.ID]
	if !ok || !strings.Contains(f.Evidence, "our floor is €1.6M") || !strings.Contains(f.Message, "Jane Roe") {
		t.Fatalf("got %+v", f)
	}
}

func TestHiddenTextButNotHiddenParagraphMarks(t *testing.T) {
	b := docx(t, `<w:p><w:pPr><w:rPr><w:vanish/></w:rPr></w:pPr><w:r><w:t>Visible paragraph</w:t></w:r></w:p>
		<w:p><w:r><w:rPr><w:vanish/></w:rPr><w:t>Internal: client is Northwind</w:t></w:r><w:r><w:t> shown</w:t></w:r></w:p>
		<w:p><w:r><w:rPr><w:vanish w:val="0"/></w:rPr><w:t>explicitly not hidden</w:t></w:r></w:p>`, nil)
	f, ok := inspect(t, b)[leak.HiddenContent.ID]
	if !ok || f.Evidence != `"Internal: client is Northwind"` {
		t.Fatalf("got %+v", f)
	}
}

func TestCleanDocument(t *testing.T) {
	b := docx(t, `<w:p><w:r><w:t>Nothing to see.</w:t></w:r></w:p>`, nil)
	if fs := inspect(t, b); len(fs) != 0 {
		t.Fatalf("got %+v", fs)
	}
}

func TestPropertiesCommentsAndTemplatePath(t *testing.T) {
	b := docx(t, `<w:p><w:r><w:t>Offer</w:t></w:r></w:p>`, map[string]string{
		"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/">
			<dc:title>Offer</dc:title><dc:creator>Jane Roe</dc:creator><cp:lastModifiedBy>Tom Lee</cp:lastModifiedBy>
			<cp:revision>14</cp:revision><dcterms:created>2024-05-01T08:12:00Z</dcterms:created></cp:coreProperties>`,
		"docProps/app.xml": `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">
			<Template>Normal.dotm</Template><TotalTime>1830</TotalTime><Application>Microsoft Office Word</Application><Company>Acme SA</Company></Properties>`,
		"word/comments.xml": `<w:comments ` + wNS + `><w:comment w:id="0" w:author="Legal"><w:p><w:r><w:t>Do not send this version to Halvorsen</w:t></w:r></w:p></w:comment></w:comments>`,
		"word/_rels/settings.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate" Target="file:///C:\Users\jroe\AppData\Roaming\Microsoft\Templates\Acme%20Offer.dotx" TargetMode="External"/></Relationships>`,
	})
	r := inspect(t, b)
	if f := r[leak.PersonalNames.ID]; !strings.Contains(f.Evidence, "author: Jane Roe") || !strings.Contains(f.Evidence, "last saved by: Tom Lee") || !strings.Contains(f.Evidence, "company: Acme SA") {
		t.Errorf("names: %+v", f)
	}
	if f := r[leak.Comments.ID]; !strings.Contains(f.Message, "Legal") || !strings.Contains(f.Evidence, "Halvorsen") {
		t.Errorf("comments: %+v", f)
	}
	if f := r[leak.LocalPaths.ID]; !strings.Contains(f.Evidence, `C:\Users\jroe`) {
		t.Errorf("paths: %+v", f)
	}
	if f := r[leak.SoftwareTrail.ID]; !strings.Contains(f.Evidence, "saved 14 times") || !strings.Contains(f.Evidence, "edited for 30 hours") {
		t.Errorf("trail: %+v", f)
	}

	sealed, err := Seal(b)
	if err != nil {
		t.Fatal(err)
	}
	left := inspect(t, sealed)
	for _, gone := range []*leak.Rule{leak.PersonalNames, leak.LocalPaths, leak.SoftwareTrail, leak.Timestamps} {
		if f, ok := left[gone.ID]; ok {
			t.Errorf("%s survived sealing: %+v", gone.Name, f)
		}
	}
	if _, ok := left[leak.Comments.ID]; !ok {
		t.Error("comments are content and must survive sealing (and stay reported)")
	}
	p, _ := open(sealed)
	if !strings.Contains(string(p.data("docProps/core.xml")), "<dc:title>Offer</dc:title>") {
		t.Error("the title is kept")
	}
}

func TestExcelHiddenSheetsRowsAndPivotCache(t *testing.T) {
	b := build(t, map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` + rNS + `><sheets>
			<sheet name="Summary" sheetId="1" r:id="rId1"/><sheet name="Salaries" sheetId="2" state="veryHidden" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId1" Type="x" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="x" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Total</t></si><si><t>Jane Roe</t></si><si><r><t>Bonus </t></r><r><t>2024</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
			<row r="1"><c r="A1" t="s"><v>0</v></c></row><row r="2" hidden="1"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>15000</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
			<row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1"><v>98000</v></c></row></sheetData></worksheet>`,
		"xl/pivotCache/pivotCacheRecords1.xml": `<pivotCacheRecords xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><r><n v="1"/></r><r><n v="2"/></r></pivotCacheRecords>`,
	})
	f, ok := inspect(t, b)[leak.HiddenContent.ID]
	if !ok {
		t.Fatal("no hidden content reported")
	}
	for _, want := range []string{`very hidden sheet "Salaries"`, "1 hidden row in sheet1.xml", "pivot table cache holding a full copy of 2 source rows"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q: %s", want, f.Message)
		}
	}
	if !strings.Contains(f.Evidence, "Jane Roe 98000") || !strings.Contains(f.Evidence, "Bonus 2024 15000") {
		t.Errorf("evidence: %s", f.Evidence)
	}
	arts, _ := Reveal(b)
	found := false
	for _, a := range arts {
		if a.Name == "hidden-sheet-Salaries.txt" && string(a.Data) == "Jane Roe\n98000\n" {
			found = true
		}
	}
	if !found {
		t.Errorf("hidden sheet not revealed: %+v", arts)
	}
}

func TestPowerPointNotesAndHiddenSlides(t *testing.T) {
	a := `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	b := build(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation ` + a + `/>`,
		"ppt/slides/slide1.xml":           `<p:sld ` + a + `><a:t>Roadmap</a:t></p:sld>`,
		"ppt/slides/slide2.xml":           `<p:sld ` + a + ` show="0"><a:t>Acquisition target: Contoso</a:t></p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<p:notes ` + a + `><p:sp><p:nvPr><p:ph type="sldNum"/></p:nvPr><a:t>1</a:t></p:sp><p:sp><p:nvPr><p:ph type="body"/></p:nvPr><a:t>Don't mention the layoffs</a:t></p:sp></p:notes>`,
	})
	f, ok := inspect(t, b)[leak.HiddenContent.ID]
	if !ok || !strings.Contains(f.Evidence, "Acquisition target: Contoso") || !strings.Contains(f.Evidence, "Don't mention the layoffs") || strings.Contains(f.Evidence, "Roadmap") {
		t.Fatalf("got %+v", f)
	}
}

func TestPhotoInsideDocument(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(1, 1, color.Black)
	var j bytes.Buffer
	jpeg.Encode(&j, img, nil)
	tiff := exif.Encode(nil, nil, []exif.Field{
		exif.ASCII(1, "N"), exif.Rationals(2, [2]uint32{51, 1}, [2]uint32{30, 1}, [2]uint32{0, 1}),
		exif.ASCII(3, "W"), exif.Rationals(4, [2]uint32{0, 1}, [2]uint32{7, 1}, [2]uint32{0, 1}),
	}, nil)
	seg := append([]byte{0xFF, 0xE1, 0, 0}, append([]byte("Exif\x00\x00"), tiff...)...)
	seg[2], seg[3] = byte((len(seg)-2)>>8), byte(len(seg)-2)
	photo := append([]byte{0xFF, 0xD8}, append(seg, j.Bytes()[2:]...)...)

	b := docx(t, `<w:p/>`, map[string]string{"word/media/image1.jpeg": string(photo)})
	f, ok := inspect(t, b)[leak.GPSLocation.ID]
	if !ok || f.Location != "word/media/image1.jpeg" || !strings.HasPrefix(f.Message, "A picture inside the document") {
		t.Fatalf("got %+v", f)
	}
	sealed, _ := Seal(b)
	if _, ok := inspect(t, sealed)[leak.GPSLocation.ID]; ok {
		t.Fatal("sealing must clean pictures inside the document")
	}
}

func TestNotAZipOfOffice(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("readme.txt")
	f.Write([]byte("hi"))
	w.Close()
	if _, err := Inspect(b.Bytes()); err == nil {
		t.Fatal("expected an error")
	}
}
