package sniff

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]Format{
		"%PDF-1.7\n1 0 obj":                        PDF,
		"\x00\x05junk\xff%PDF-1.4\n":               PDF,
		"package pdf\n\nconst h = \"%PDF-1.7\\n\"": Unknown,
		"%PDF-x":                             Unknown,
		"\x89PNG\r\n\x1a\n....":              PNG,
		"\xff\xd8\xff\xe0":                   JPEG,
		"PK\x03\x04....word/document.xml":    DOCX,
		"PK\x03\x04....xl/workbook.xml":      XLSX,
		"PK\x03\x04....ppt/presentation.xml": PPTX,
		"PK\x03\x04....readme.txt":           ZIP,
		"\x00\x00\x00\x18ftypmp42":           MP4,
		"hello":                              Unknown,
	}
	for in, want := range cases {
		if got := Detect([]byte(in)); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
