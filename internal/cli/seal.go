package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xceg/sphinx"
	"github.com/xceg/sphinx/internal/render"
)

func seal(args []string, env Env) int {
	fs := newFlags("seal", "seal [-o DIR | --in-place] FILE...", env)
	dir := fs.String("o", "", "directory to write sealed copies into (default: FILE.sealed.EXT next to each file)")
	inPlace := fs.Bool("in-place", false, "overwrite the original files")
	failOn := fs.String("fail-on", "medium", "exit 1 when a leak at or above this severity survives sealing")
	noColor := fs.Bool("no-color", false, "disable colours (also honours NO_COLOR)")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if *inPlace && *dir != "" {
		return usageError(env, errors.New("-o and --in-place cannot be combined"))
	}
	threshold, err := thresholdOf(*failOn)
	if err != nil {
		return usageError(env, err)
	}
	targets, err := expand(fs.Args())
	if err != nil {
		return usageError(env, err)
	}
	p := render.Palette(useColor(env, *noColor))
	code := ExitClean
	for _, t := range targets {
		if t.path == "-" {
			fmt.Fprintln(env.Stderr, "sphinx: seal needs a file path, not standard input")
			code = ExitUsage
			continue
		}
		left, err := sealOne(t, *dir, *inPlace, env, p)
		if err != nil {
			fmt.Fprintf(env.Stderr, "sphinx: %s: %v\n", t.path, err)
			code = max(code, ExitError)
			continue
		}
		for _, f := range left {
			if f.Severity >= threshold {
				code = ExitFindings
			}
		}
	}
	return code
}

func sealOne(t target, dir string, inPlace bool, env Env, p render.Colors) ([]sphinx.Finding, error) {
	data, err := os.ReadFile(t.path)
	if err != nil {
		return nil, err
	}
	before, err := sphinx.Inspect(data)
	if err != nil {
		return nil, err
	}
	sealed, err := sphinx.Seal(data)
	if errors.Is(err, sphinx.ErrUnsupported) {
		return nil, fmt.Errorf("Sphinx cannot seal this kind of file yet; see `sphinx gaze` for what to fix by hand")
	}
	if err != nil {
		return nil, err
	}
	after, err := sphinx.Inspect(sealed)
	if err != nil {
		return nil, fmt.Errorf("sealed copy does not parse: %w", err)
	}
	out := sealedPath(t.path, dir, inPlace)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(t.path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomic(out, sealed, mode); err != nil {
		return nil, err
	}
	removed := len(before) - len(after)
	fmt.Fprintf(env.Stdout, "%s → %s  %s\n", t.path, out, p.Dim(fmt.Sprintf("(%d leak%s removed, %s → %s)", removed, plural(removed), size(len(data)), size(len(sealed)))))
	for _, f := range after {
		fmt.Fprintf(env.Stdout, "  %s %s: %s\n", p.Severity(f.Severity), sphinx.LookupRule(f.Rule).Title, f.Message)
	}
	return after, nil
}

func sealedPath(path, dir string, inPlace bool) string {
	if inPlace {
		return path
	}
	base := filepath.Base(path)
	if dir != "" {
		return filepath.Join(dir, base)
	}
	ext := filepath.Ext(base)
	return filepath.Join(filepath.Dir(path), strings.TrimSuffix(base, ext)+".sealed"+ext)
}

// writeAtomic writes through a temporary file so that an interrupted
// --in-place run never leaves a half-written original behind.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sphinx-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
