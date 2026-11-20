package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xceg/sphinx"
)

func reveal(args []string, env Env) int {
	fs := newFlags("reveal", "reveal [-o DIR] FILE...", env)
	dir := fs.String("o", "", "directory to write into (default: FILE.revealed next to each file)")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	targets, err := expand(fs.Args(), nil)
	if err != nil {
		return usageError(env, err)
	}
	code := ExitClean
	for _, t := range targets {
		if err := revealOne(t, *dir, env); err != nil {
			fmt.Fprintf(env.Stderr, "sphinx: %s: %v\n", t.path, err)
			code = ExitError
		}
	}
	return code
}

func revealOne(t target, dir string, env Env) error {
	data, err := load(t, env.Stdin)
	if err != nil {
		return err
	}
	arts, err := sphinx.Reveal(data)
	if errors.Is(err, sphinx.ErrUnsupported) {
		return fmt.Errorf("not a format Sphinx reads")
	}
	if err != nil {
		return err
	}
	if len(arts) == 0 {
		fmt.Fprintf(env.Stdout, "%s: nothing hidden to extract\n", t.path)
		return nil
	}
	base := filepath.Base(t.path)
	if t.path == "-" {
		base = "stdin"
	}
	if dir == "" {
		dir = filepath.Join(filepath.Dir(t.path), base+".revealed")
		if t.path == "-" {
			dir = "stdin.revealed"
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fmt.Fprintf(env.Stdout, "%s gives up %d hidden part%s:\n", t.path, len(arts), plural(len(arts)))
	for _, a := range arts {
		p := filepath.Join(dir, stem+"."+a.Name)
		if err := os.WriteFile(p, a.Data, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "  %s  (%s)\n", p, a.Note)
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
