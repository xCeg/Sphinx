package ooxml

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/xceg/sphinx/internal/leak"
)

func Reveal(b []byte) ([]leak.Artifact, error) {
	p, err := open(b)
	if err != nil {
		return nil, err
	}
	r := analyze(p)
	var out []leak.Artifact
	text := func(name, note string, ps []passage) {
		if len(ps) == 0 {
			return
		}
		var sb strings.Builder
		for _, x := range ps {
			if x.Author != "" {
				fmt.Fprintf(&sb, "[%s] %s: %s\n", x.Where, x.Author, x.Text)
			} else {
				fmt.Fprintf(&sb, "[%s] %s\n", x.Where, x.Text)
			}
		}
		out = append(out, leak.Artifact{Name: name, Note: note, Data: []byte(sb.String())})
	}
	text("deleted-text.txt", "text deleted under Track Changes", r.deleted)
	text("hidden.txt", "content hidden from view", r.hidden)
	text("comments.txt", "review comments with their authors", r.comments)
	var names []string
	for n := range r.hiddenParts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, leak.Artifact{Name: n, Note: "content hidden from view", Data: r.hiddenParts[n]})
	}
	var parts []string
	for name := range p.parts {
		if path.Base(path.Dir(name)) == "embeddings" || strings.HasPrefix(name, "docProps/thumbnail") {
			parts = append(parts, name)
		}
	}
	sort.Strings(parts)
	for _, name := range parts {
		note := "object embedded in the document"
		if strings.HasPrefix(name, "docProps/") {
			note = "preview image of the first page"
		}
		out = append(out, leak.Artifact{Name: path.Base(name), Note: note, Data: p.data(name)})
	}
	return out, nil
}
