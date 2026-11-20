package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/xceg/sphinx"
)

// Colors applies ANSI styles when enabled.
type Colors struct{ on bool }

func Palette(on bool) Colors { return Colors{on} }

func (p Colors) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p Colors) Bold(s string) string { return p.wrap("1", s) }
func (p Colors) Dim(s string) string  { return p.wrap("2", s) }

func (p Colors) Severity(s sphinx.Severity) string {
	label := fmt.Sprintf("%-8s", s)
	switch s {
	case sphinx.Critical:
		return p.wrap("1;31", label)
	case sphinx.High:
		return p.wrap("31", label)
	case sphinx.Medium:
		return p.wrap("33", label)
	}
	return p.wrap("36", label)
}

func (p Colors) Verdict(v sphinx.Verdict) string {
	label := strings.ToUpper(string(v))
	switch v {
	case sphinx.Betrays:
		return p.wrap("1;31", label)
	case sphinx.Whispers:
		return p.wrap("1;33", label)
	}
	return p.wrap("1;32", label)
}

const indent = "    "

func Text(w io.Writer, reports []Report, o Options) error {
	p := Palette(o.Color)
	var betrays, whispers, silent, failed int
	for _, r := range reports {
		switch {
		case r.Error != "":
			failed++
		case r.Verdict == sphinx.Betrays:
			betrays++
		case r.Verdict == sphinx.Whispers:
			whispers++
		default:
			silent++
		}
		if o.Quiet && r.Error == "" && len(r.Findings) == 0 {
			continue
		}
		if r.Error != "" {
			fmt.Fprintf(w, "%s  %s\n%s%s\n\n", p.Bold(r.Path), p.wrap("1;35", "UNREADABLE"), indent, r.Error)
			continue
		}
		fmt.Fprintf(w, "%s  %s  %s\n", p.Bold(r.Path), p.Dim(FormatName(r.Format)+" ·"), p.Verdict(r.Verdict))
		for _, f := range r.Findings {
			rule := sphinx.LookupRule(f.Rule)
			fmt.Fprintf(w, "%s%s %s  %s\n", indent, p.Severity(f.Severity), rule.Title, p.Dim(f.Rule))
			fmt.Fprintf(w, "%s%s %s\n", indent, strings.Repeat(" ", 8), wrap(f.Message, 72, indent+strings.Repeat(" ", 9)))
			if f.Evidence != "" {
				fmt.Fprintf(w, "%s%s %s %s\n", indent, strings.Repeat(" ", 8), p.Dim("›"), wrap(f.Evidence, 70, indent+strings.Repeat(" ", 11)))
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, summary(p, len(reports), betrays, whispers, silent, failed))
	if betrays+whispers > 0 {
		fmt.Fprintln(w, p.Dim("See what they hide with `sphinx reveal FILE`; write clean copies with `sphinx seal FILE`."))
	}
	return nil
}

func summary(p Colors, total, betrays, whispers, silent, failed int) string {
	var parts []string
	if betrays > 0 {
		parts = append(parts, p.wrap("31", fmt.Sprintf("%d betray%s you", betrays, plural(betrays, "s", ""))))
	}
	if whispers > 0 {
		parts = append(parts, p.wrap("33", fmt.Sprintf("%d whisper%s", whispers, plural(whispers, "s", ""))))
	}
	if silent > 0 {
		parts = append(parts, p.wrap("32", fmt.Sprintf("%d keep%s silent", silent, plural(silent, "s", ""))))
	}
	if failed > 0 {
		parts = append(parts, p.wrap("35", fmt.Sprintf("%d could not be read", failed)))
	}
	head := fmt.Sprintf("The Sphinx questioned %d file%s", total, plural(total, "", "s"))
	if len(parts) == 0 {
		return head + "."
	}
	return head + ": " + strings.Join(parts, ", ") + "."
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// wrap breaks s into lines of at most width runes, indenting continuations.
func wrap(s string, width int, prefix string) string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"+prefix)
}
