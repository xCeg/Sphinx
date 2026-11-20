package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/xceg/sphinx"
)

func Markdown(w io.Writer, reports []Report, o Options) error {
	fmt.Fprintln(w, "### Sphinx report")
	fmt.Fprintln(w)
	shown := false
	for _, r := range reports {
		if len(r.Findings) == 0 && r.Error == "" {
			continue
		}
		shown = true
		if r.Error != "" {
			fmt.Fprintf(w, "**`%s`** could not be read: %s\n\n", r.Path, cell(r.Error))
			continue
		}
		fmt.Fprintf(w, "**`%s`** %s: %s\n\n", r.Path, FormatName(r.Format), r.Verdict)
		fmt.Fprintln(w, "| Severity | Rule | What it gives away | Evidence |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, f := range r.Findings {
			rule := sphinx.LookupRule(f.Rule)
			fmt.Fprintf(w, "| %s | `%s` %s | %s | %s |\n", f.Severity, f.Rule, rule.Name, cell(f.Message), cell(f.Evidence))
		}
		fmt.Fprintln(w)
	}
	if !shown {
		fmt.Fprintf(w, "All %d files keep silent.\n", len(reports))
	}
	return nil
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
