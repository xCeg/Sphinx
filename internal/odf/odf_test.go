package odf

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

func rules(t *testing.T, b []byte) map[string]leak.Finding {
	t.Helper()
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]leak.Finding{}
	for _, f := range fs {
		m[f.Rule] = f
	}
	return m
}

// testdata/offer-letter.odt is site/examples/offer-letter.docx converted by
// LibreOffice 24.2, which keeps the tracked deletion, the comment and the
// document properties.
func TestLibreOfficeText(t *testing.T) {
	b, err := os.ReadFile("testdata/offer-letter.odt")
	if err != nil {
		t.Fatal(err)
	}
	r := rules(t, b)
	if !strings.Contains(r[leak.DeletedText.ID].Evidence, "Our ceiling for this role is €72,000") || !strings.Contains(r[leak.DeletedText.ID].Message, "Tom Becker") {
		t.Errorf("deleted: %+v", r[leak.DeletedText.ID])
	}
	if !strings.Contains(r[leak.Comments.ID].Message, "Ines Morel") {
		t.Errorf("comments: %+v", r[leak.Comments.ID])
	}
	if !strings.Contains(r[leak.PersonalNames.ID].Evidence, "Company: Lantern Works") {
		t.Errorf("names: %+v", r[leak.PersonalNames.ID])
	}
	sealed, err := Seal(b)
	if err != nil {
		t.Fatal(err)
	}
	left := rules(t, sealed)
	for _, gone := range []*leak.Rule{leak.PersonalNames, leak.Timestamps} {
		if f, ok := left[gone.ID]; ok {
			t.Errorf("%s survived sealing: %+v", gone.Name, f)
		}
	}
	if _, ok := left[leak.DeletedText.ID]; !ok {
		t.Error("tracked changes are content and must stay reported")
	}
	zr, _ := zip.NewReader(bytes.NewReader(sealed), int64(len(sealed)))
	if zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store || zr.File[0].Flags&0x8 != 0 {
		t.Fatal("mimetype must stay first, stored, without a data descriptor")
	}
}

func build(t *testing.T, content string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	f.Write([]byte("application/vnd.oasis.opendocument.spreadsheet"))
	f, _ = w.Create("content.xml")
	f.Write([]byte(content))
	w.Close()
	return b.Bytes()
}

const ns = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0"`

func TestHiddenSheetsRowsAndSections(t *testing.T) {
	b := build(t, `<office:document-content `+ns+`>
	<office:automatic-styles><style:style style:name="ta2"><style:table-properties table:display="false"/></style:style></office:automatic-styles>
	<office:body><office:spreadsheet>
	<table:table table:name="Public"><table:table-row><table:table-cell><text:p>Total</text:p></table:table-cell></table:table-row>
	<table:table-row table:visibility="collapse"><table:table-cell><text:p>Bonus pool 40k</text:p></table:table-cell></table:table-row></table:table>
	<table:table table:name="Salaries" table:style-name="ta2"><table:table-row><table:table-cell><text:p>Jane 98000</text:p></table:table-cell></table:table-row></table:table>
	</office:spreadsheet></office:body></office:document-content>`)
	f := rules(t, b)[leak.HiddenContent.ID]
	for _, want := range []string{`hidden sheet "Salaries"`, "hidden row"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q: %s", want, f.Message)
		}
	}
	if !strings.Contains(f.Evidence, "Jane 98000") || !strings.Contains(f.Evidence, "Bonus pool 40k") || strings.Contains(f.Evidence, "Total") {
		t.Errorf("evidence: %s", f.Evidence)
	}
}
