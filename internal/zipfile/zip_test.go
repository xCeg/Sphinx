package zipfile

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

// testdata/DS_Store was written by the ds_store 1.3.4 Python package, the
// library dmgbuild uses, for a folder holding four files.
func TestDSStoreNames(t *testing.T) {
	b, err := os.ReadFile("testdata/DS_Store")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(dsStoreNames(b), "|")
	want := "holiday.jpg|layoff-list-draft.pdf|offer-letter.docx|salaries-2025-CONFIDENTIAL.xlsx"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func archive(t *testing.T, files map[string][]byte) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range files {
		f, _ := w.Create(name)
		f.Write(data)
	}
	w.Close()
	return b.Bytes()
}

var fake = Engine{
	Inspect: func(b []byte) ([]leak.Finding, error) {
		if bytes.Contains(b, []byte("SECRET")) {
			return []leak.Finding{leak.EmbeddedText.Find("note", "SECRET")}, nil
		}
		return nil, nil
	},
	Reveal: func([]byte) ([]leak.Artifact, error) { return nil, nil },
	Seal:   func(b []byte) ([]byte, error) { return bytes.ReplaceAll(b, []byte("SECRET"), nil), nil },
}

func TestArchiveFromAMac(t *testing.T) {
	ds, _ := os.ReadFile("testdata/DS_Store")
	b := archive(t, map[string][]byte{
		"Shared/offer-letter.docx":         []byte("letter"),
		"Shared/holiday.jpg":               []byte("SECRET photo"),
		"Shared/.DS_Store":                 ds,
		"__MACOSX/Shared/._holiday.jpg":    []byte("apple double"),
		"Users/jroe/Desktop/Shared/readme": []byte("hi"),
	})
	fs, err := Inspect(b, fake)
	if err != nil {
		t.Fatal(err)
	}
	r := map[string]leak.Finding{}
	for _, f := range fs {
		r[f.Rule] = f
	}
	j := r[leak.ArchiveJunk.ID]
	if j.Severity != leak.High || !strings.Contains(j.Message, "2 files that are not in the archive") || !strings.Contains(j.Evidence, "layoff-list-draft.pdf") {
		t.Fatalf("junk: %+v", j)
	}
	if !strings.Contains(r[leak.LocalPaths.ID].Evidence, "Users/jroe") {
		t.Fatalf("paths: %+v", r[leak.LocalPaths.ID])
	}
	if r[leak.EmbeddedText.ID].Location != "Shared/holiday.jpg" {
		t.Fatalf("nested findings carry the member name: %+v", r[leak.EmbeddedText.ID])
	}

	sealed, err := Seal(b, fake)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(sealed), int64(len(sealed)))
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); strings.Contains(got, "DS_Store") || strings.Contains(got, "__MACOSX") {
		t.Fatalf("junk survived sealing: %s", got)
	}
	left, _ := Inspect(sealed, fake)
	for _, f := range left {
		if f.Rule == leak.EmbeddedText.ID {
			t.Fatal("members must be sealed too")
		}
	}
}
