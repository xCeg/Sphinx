package pdf

import (
	"fmt"
	"path"
	"strings"

	"github.com/xceg/sphinx/internal/leak"
)

func Inspect(b []byte) ([]leak.Finding, error) {
	a, err := Analyze(b)
	if err != nil {
		return nil, err
	}
	return a.Findings, nil
}

func Reveal(b []byte) ([]leak.Artifact, error) {
	a, err := Analyze(b)
	if err != nil {
		return nil, err
	}
	var out []leak.Artifact
	if len(a.Covered)+len(a.Redactions) > 0 {
		var sb strings.Builder
		for _, c := range a.Redactions {
			fmt.Fprintf(&sb, "page %d, marked for redaction: %s\n", c.Page, c.Text)
		}
		for _, c := range a.Covered {
			fmt.Fprintf(&sb, "page %d, under a box: %s\n", c.Page, c.Text)
		}
		out = append(out, leak.Artifact{Name: "hidden-text.txt", Note: "text that is painted over but still in the file", Data: []byte(sb.String())})
	}
	for _, r := range a.Revisions {
		out = append(out, leak.Artifact{
			Name: fmt.Sprintf("revision-%d.pdf", r.Number),
			Note: fmt.Sprintf("the document as saved in revision %d", r.Number),
			Data: r.Bytes,
		})
	}
	if len(a.Comments) > 0 {
		out = append(out, leak.Artifact{Name: "comments.txt", Note: "review comments", Data: []byte(strings.Join(a.Comments, "\n") + "\n")})
	}
	for i, at := range a.Attachments {
		if at.Data == nil {
			continue
		}
		name := path.Base(strings.ReplaceAll(at.Name, "\\", "/"))
		if name == "" || name == "." || name == "/" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		out = append(out, leak.Artifact{Name: "attachment-" + name, Note: "file attached inside the PDF", Data: at.Data})
	}
	return out, nil
}
