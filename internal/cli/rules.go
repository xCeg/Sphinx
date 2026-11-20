package cli

import (
	"fmt"

	"github.com/xceg/sphinx"
)

func rules(args []string, env Env) int {
	if len(args) == 0 {
		for _, r := range sphinx.Rules() {
			fmt.Fprintf(env.Stdout, "%s  %-20s %-8s  %s\n", r.ID, r.Name, r.Severity, r.Title)
		}
		fmt.Fprintln(env.Stdout, "\nRun `sphinx rules RULE` for the full explanation.")
		return ExitClean
	}
	code := ExitClean
	for _, a := range args {
		r := sphinx.LookupRule(a)
		if r == nil {
			fmt.Fprintf(env.Stderr, "sphinx: no rule named %q\n", a)
			code = ExitUsage
			continue
		}
		fmt.Fprintf(env.Stdout, "%s %s (%s)\n%s\n\nWhy it matters\n  %s\n\nWhat to do\n  %s\n\n",
			r.ID, r.Name, r.Severity, r.Title, r.Why, r.Fix)
	}
	return code
}
