package cli

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xceg/sphinx"
)

const maxFileSize = 1 << 30

// target is one file to process. Explicit paths are always processed and
// reported even if unsupported; files found by walking a directory are
// only kept when Sphinx recognises their content.
type target struct {
	path     string
	explicit bool
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".hg": true, ".svn": true, "vendor": true, ".venv": true, "__pycache__": true}

// excluded reports whether a path found while walking matches one of the
// --exclude patterns, tried against both the full path and the base name.
func excluded(p string, patterns []string) bool {
	slash := filepath.ToSlash(p)
	for _, pat := range patterns {
		pat = filepath.ToSlash(strings.TrimSuffix(pat, "/"))
		if ok, _ := path.Match(pat, slash); ok {
			return true
		}
		if ok, _ := path.Match(pat, path.Base(slash)); ok {
			return true
		}
		if strings.HasPrefix(slash, pat+"/") || strings.Contains(slash, "/"+pat+"/") {
			return true
		}
	}
	return false
}

func expand(args, exclude []string) ([]target, error) {
	var out []target
	for _, a := range args {
		if a == "-" {
			out = append(out, target{path: "-", explicit: true})
			continue
		}
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, target{path: a, explicit: true})
			continue
		}
		var found []string
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != a && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || excluded(p, exclude)) {
					return filepath.SkipDir
				}
				return nil
			}
			if excluded(p, exclude) {
				return nil
			}
			if d.Type().IsRegular() && supported(p) {
				found = append(found, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(found)
		for _, p := range found {
			out = append(out, target{path: p})
		}
	}
	return out, nil
}

func supported(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	if sphinx.Format(head[:n]) != "" {
		return true
	}
	// Office files are zips whose telling part names can sit past the
	// first few kilobytes, behind a large first entry.
	if !bytes.HasPrefix(head[:n], []byte("PK\x03\x04")) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && sphinx.Format(data) != ""
}

func load(t target, stdin io.Reader) ([]byte, error) {
	if t.path == "-" {
		return io.ReadAll(io.LimitReader(stdin, maxFileSize))
	}
	info, err := os.Stat(t.path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("file is larger than %d MB", maxFileSize>>20)
	}
	return os.ReadFile(t.path)
}
