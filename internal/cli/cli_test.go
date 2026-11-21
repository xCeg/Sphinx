package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type run struct {
	code           int
	stdout, stderr string
}

func invoke(t *testing.T, env map[string]string, args ...string) run {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Getenv: func(k string) string { return env[k] },
	})
	return run{code, out.String(), errb.String()}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "pdf", "testdata", "chromium-boxes.pdf")
	if name == "screenshot.png" {
		src = filepath.Join("..", "pngfile", "testdata", "screenshot.png")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGazeTextAndExitCodes(t *testing.T) {
	pdf := fixture(t, "memo.pdf")
	r := invoke(t, nil, pdf)
	if r.code != ExitFindings || !strings.Contains(r.stdout, "BETRAYS") || !strings.Contains(r.stdout, "Halvorsen Maritime AS") {
		t.Fatalf("code %d\n%s", r.code, r.stdout)
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("no colours when stdout is not a terminal")
	}
	if r := invoke(t, nil, "--fail-on", "never", pdf); r.code != ExitClean {
		t.Fatalf("--fail-on never: code %d", r.code)
	}
	clean := fixture(t, "screenshot.png")
	if r := invoke(t, nil, clean); r.code != ExitClean || !strings.Contains(r.stdout, "SILENT") {
		t.Fatalf("clean file: code %d\n%s", r.code, r.stdout)
	}
}

func TestColours(t *testing.T) {
	pdf := fixture(t, "memo.pdf")
	if r := invoke(t, map[string]string{"FORCE_COLOR": "1"}, pdf); !strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("FORCE_COLOR should enable colours")
	}
	if r := invoke(t, map[string]string{"FORCE_COLOR": "1", "NO_COLOR": "1"}, pdf); strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("NO_COLOR wins")
	}
}

func TestJSONAndSARIF(t *testing.T) {
	pdf := fixture(t, "memo.pdf")
	var doc struct {
		Files []struct {
			Verdict  string
			Findings []struct{ Rule, Severity string }
		}
	}
	r := invoke(t, nil, pdf, "--format", "json")
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Files[0].Verdict != "betrays" || doc.Files[0].Findings[0].Severity != "critical" {
		t.Fatalf("%+v", doc)
	}
	var sarif struct {
		Version string
		Runs    []struct {
			Results []struct {
				RuleID string
				Level  string
			}
		}
	}
	r = invoke(t, nil, "--format=sarif", pdf)
	if err := json.Unmarshal([]byte(r.stdout), &sarif); err != nil {
		t.Fatal(err)
	}
	if sarif.Version != "2.1.0" || sarif.Runs[0].Results[0].RuleID != "SPX002" || sarif.Runs[0].Results[0].Level != "error" {
		t.Fatalf("%+v", sarif)
	}
}

func TestDirectoryWalkSkipsUnsupported(t *testing.T) {
	dir := filepath.Dir(fixture(t, "memo.pdf"))
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "x.pdf"), []byte("%PDF-1.4"), 0o644)
	r := invoke(t, nil, "--format", "json", dir)
	if strings.Count(r.stdout, `"path"`) != 1 {
		t.Fatalf("expected only memo.pdf:\n%s", r.stdout)
	}
}

func TestUnsupportedExplicitFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	r := invoke(t, nil, p)
	if r.code != ExitError || !strings.Contains(r.stdout, "UNREADABLE") {
		t.Fatalf("code %d\n%s", r.code, r.stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"--format", "xml", "x"}, {"--fail-on", "sometimes", "x"}, {"does-not-exist.pdf"}, {"reveal"}} {
		if r := invoke(t, nil, args...); r.code != ExitUsage {
			t.Errorf("%v: code %d", args, r.code)
		}
	}
}

func TestRevealAndSeal(t *testing.T) {
	pdf := fixture(t, "memo.pdf")
	out := filepath.Join(t.TempDir(), "out")
	if r := invoke(t, nil, "reveal", "-o", out, pdf); r.code != ExitClean {
		t.Fatalf("reveal: %d %s", r.code, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "memo.hidden-text.txt"))
	if err != nil || !strings.Contains(string(b), "Ingrid Halvorsen") {
		t.Fatalf("revealed text: %q %v", b, err)
	}
	if r := invoke(t, nil, "seal", pdf); r.code != ExitFindings || !strings.Contains(r.stdout, "Blacked-out text is still in the file") {
		t.Fatalf("sealing keeps text under boxes and must say so: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	png := fixture(t, "screenshot.png")
	if r := invoke(t, nil, "seal", png); r.code != ExitClean {
		t.Fatalf("seal png: %d %s", r.code, r.stderr)
	}
	if _, err := os.Stat(strings.TrimSuffix(png, ".png") + ".sealed.png"); err != nil {
		t.Fatal(err)
	}
}

func TestRules(t *testing.T) {
	if r := invoke(t, nil, "rules"); r.code != 0 || !strings.Contains(r.stdout, "SPX001") {
		t.Fatal(r.stdout)
	}
	if r := invoke(t, nil, "rules", "hidden-crop"); !strings.Contains(r.stdout, "CVE-2023-21036") {
		t.Fatal(r.stdout)
	}
}

func TestFlagsAfterArguments(t *testing.T) {
	pdf := fixture(t, "memo.pdf")
	if r := invoke(t, nil, pdf, "--format", "json"); !strings.HasPrefix(r.stdout, "{") {
		t.Fatalf("flags after the file should apply:\n%s", r.stdout)
	}
}

func TestExclude(t *testing.T) {
	dir := t.TempDir()
	b, _ := os.ReadFile(filepath.Join("..", "pdf", "testdata", "chromium-boxes.pdf"))
	os.MkdirAll(filepath.Join(dir, "testdata"), 0o755)
	os.WriteFile(filepath.Join(dir, "testdata", "a.pdf"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "b.pdf"), b, 0o644)
	r := invoke(t, nil, "--format", "json", "--exclude", "testdata", "--exclude", "*.png", dir)
	if strings.Count(r.stdout, `"path"`) != 1 || !strings.Contains(r.stdout, "b.pdf") {
		t.Fatalf("got:\n%s", r.stdout)
	}
}

func TestWalkFindsOfficeFileWithLargeFirstEntry(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "[Content_Types].xml", Method: zip.Store})
	w.Write([]byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + strings.Repeat(" ", 8000) + `</Types>`))
	w, _ = zw.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`))
	zw.Close()
	os.WriteFile(filepath.Join(dir, "big.docx"), b.Bytes(), 0o644)
	if r := invoke(t, nil, "--format", "json", dir); !strings.Contains(r.stdout, "big.docx") {
		t.Fatalf("walk skipped the document:\n%s", r.stdout)
	}
}
