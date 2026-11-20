package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/xceg/sphinx"
	"github.com/xceg/sphinx/internal/render"
)

func gaze(args []string, env Env) int {
	fs := newFlags("gaze", "[gaze] [flags] FILE|DIR...", env)
	format := fs.String("format", "text", "output format: text, json, sarif or markdown")
	failOn := fs.String("fail-on", "medium", "exit 1 when a leak at or above this severity is found (low, medium, high, critical, never)")
	minSev := fs.String("min-severity", "low", "hide leaks below this severity")
	noColor := fs.Bool("no-color", false, "disable colours (also honours NO_COLOR)")
	quiet := fs.Bool("quiet", false, "only list files that leak or could not be read")
	var exclude multi
	fs.Var(&exclude, "exclude", "skip paths matching this glob or directory while walking (repeatable)")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	out, err := render.Lookup(*format)
	if err != nil {
		return usageError(env, err)
	}
	threshold, err := thresholdOf(*failOn)
	if err != nil {
		return usageError(env, err)
	}
	floor, err := sphinx.ParseSeverity(*minSev)
	if err != nil {
		return usageError(env, err)
	}
	targets, err := expand(fs.Args(), exclude)
	if err != nil {
		return usageError(env, err)
	}

	var reports []render.Report
	failed, leaked := false, false
	for _, t := range targets {
		r := inspect(t, env, floor)
		if r.Error != "" {
			failed = true
		}
		for _, f := range r.Findings {
			if f.Severity >= threshold {
				leaked = true
			}
		}
		reports = append(reports, r)
	}
	if err := out(env.Stdout, reports, render.Options{Color: useColor(env, *noColor), Quiet: *quiet, Version: version()}); err != nil {
		fmt.Fprintln(env.Stderr, "sphinx:", err)
		return ExitError
	}
	switch {
	case leaked:
		return ExitFindings
	case failed:
		return ExitError
	}
	return ExitClean
}

const never = sphinx.Critical + 1

func thresholdOf(s string) (sphinx.Severity, error) {
	if s == "never" {
		return never, nil
	}
	return sphinx.ParseSeverity(s)
}

func inspect(t target, env Env, floor sphinx.Severity) render.Report {
	r := render.Report{Path: t.path, Findings: []sphinx.Finding{}}
	data, err := load(t, env.Stdin)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Size = int64(len(data))
	r.Format = sphinx.Format(data)
	fs, err := sphinx.Inspect(data)
	if err != nil {
		if errors.Is(err, sphinx.ErrUnsupported) {
			err = fmt.Errorf("not a format Sphinx reads (PNG, JPEG, PDF, DOCX, XLSX, PPTX)")
		}
		r.Error = err.Error()
		return r
	}
	for _, f := range fs {
		if f.Severity >= floor {
			r.Findings = append(r.Findings, f)
		}
	}
	r.Verdict = sphinx.VerdictOf(r.Findings)
	return r
}

func usageError(env Env, err error) int {
	fmt.Fprintln(env.Stderr, "sphinx:", err)
	return ExitUsage
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }
