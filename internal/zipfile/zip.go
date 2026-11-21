// Package zipfile inspects, reveals and seals zip archives: the files they
// carry, each inspected on its own, and the system files that come along
// uninvited.
package zipfile

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/sniff"
)

// Engine is how the archive reaches the inspectors of its members without
// an import cycle: the root package hands its own functions in.
type Engine struct {
	Inspect func([]byte) ([]leak.Finding, error)
	Reveal  func([]byte) ([]leak.Artifact, error)
	Seal    func([]byte) ([]byte, error)
}

const (
	maxMember = 256 << 20
	maxTotal  = 1 << 30
	maxDepth  = 3
)

type member struct {
	f    *zip.File
	name string
}

func open(b []byte) (*zip.Reader, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("damaged zip archive: %w", err)
	}
	return zr, nil
}

func read(f *zip.File, budget *int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, min(maxMember, *budget)))
	*budget -= int64(len(b))
	return b, err
}

// junk names the system files macOS and Windows add to folders.
func junk(name string) string {
	base := path.Base(name)
	switch {
	case strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/"):
		return "macOS resource fork"
	case base == ".DS_Store":
		return "macOS folder index"
	case strings.EqualFold(base, "Thumbs.db"):
		return "Windows thumbnail cache"
	case strings.EqualFold(base, "desktop.ini"):
		return "Windows folder settings"
	}
	return ""
}

// dsStoreNames extracts the file names recorded in a .DS_Store. Each record
// starts with a big-endian length, the name in UTF-16BE, then a four-letter
// record type; scanning for that shape avoids parsing the B-tree.
func dsStoreNames(b []byte) []string {
	seen := map[string]bool{}
	var out []string
	for p := 0; p+8 < len(b); p++ {
		n := int(b[p])<<24 | int(b[p+1])<<16 | int(b[p+2])<<8 | int(b[p+3])
		if n < 1 || n > 255 || p+4+2*n+4 > len(b) {
			continue
		}
		code := b[p+4+2*n : p+8+2*n]
		if !isRecordType(code) {
			continue
		}
		u := make([]uint16, n)
		ok := true
		for i := range u {
			u[i] = uint16(b[p+4+2*i])<<8 | uint16(b[p+5+2*i])
			if u[i] < 0x20 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		name := string(utf16.Decode(u))
		if !seen[name] && name != "." {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func isRecordType(b []byte) bool {
	for _, c := range b {
		if c < 'A' || (c > 'Z' && c < 'a') || c > 'z' {
			return false
		}
	}
	switch string(b) {
	case "Iloc", "bwsp", "lsvp", "lsvP", "icvp", "vSrn", "dilc", "fwi0", "fwsw", "fwvh", "ICVO", "LSVO", "glvp", "icgo", "icsp", "info", "logS", "lg1S", "lssp", "modD", "moDD", "phyS", "ph1S", "pict", "vstl", "BKGD", "cmmt", "dscl", "extn", "GRP0", "icvo", "icvt", "lsvo", "lsvt", "ptbL", "ptbN":
		return true
	}
	return false
}

func homePath(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "users/") || strings.HasPrefix(l, "home/") || strings.Contains(l, "/users/") || strings.Contains(l, "documents and settings/")
}

func Inspect(b []byte, e Engine) ([]leak.Finding, error) {
	return inspect(b, e, 0)
}

func inspect(b []byte, e Engine, depth int) ([]leak.Finding, error) {
	zr, err := open(b)
	if err != nil {
		return nil, err
	}
	budget := int64(maxTotal)
	present := map[string]bool{}
	for _, f := range zr.File {
		present[f.Name] = true
		present[path.Base(strings.TrimSuffix(f.Name, "/"))] = true
	}
	var out, nested []leak.Finding
	var kinds, missing, homes []string
	for _, f := range zr.File {
		if kind := junk(f.Name); kind != "" {
			kinds = append(kinds, kind)
			if path.Base(f.Name) == ".DS_Store" && !strings.Contains(f.Name, "__MACOSX") {
				data, _ := read(f, &budget)
				for _, n := range dsStoreNames(data) {
					if !present[n] {
						missing = append(missing, n)
					}
				}
			}
			continue
		}
		if homePath(f.Name) {
			homes = append(homes, f.Name)
		}
		if f.FileInfo().IsDir() || depth >= maxDepth {
			continue
		}
		data, err := read(f, &budget)
		if err != nil || len(data) == 0 {
			continue
		}
		// Plain archives are opened here, so that nesting is bounded;
		// Office files are zips too but go to their own inspector.
		var fs []leak.Finding
		if sniff.Detect(data) == sniff.ZIP {
			if depth+1 < maxDepth {
				fs, _ = inspect(data, e, depth+1)
			}
		} else {
			fs, _ = e.Inspect(data)
		}
		for _, x := range fs {
			loc := f.Name
			if x.Location != "" {
				loc += ", " + x.Location
			}
			if !strings.HasPrefix(x.Message, "In ") {
				x.Message = "In " + f.Name + ": " + x.Message
			}
			nested = append(nested, x.At(loc))
		}
	}
	if len(kinds) > 0 {
		msg := fmt.Sprintf("The archive carries system files (%s).", strings.Join(uniq(kinds), ", "))
		ev := ""
		if len(missing) > 0 {
			msg += fmt.Sprintf(" Its .DS_Store lists %d file%s that are not in the archive.", len(missing), plural(len(missing)))
			ev = strings.Join(missing[:min(len(missing), 12)], ", ")
			if len(missing) > 12 {
				ev += fmt.Sprintf(" and %d more", len(missing)-12)
			}
		}
		f := leak.ArchiveJunk.Find(msg, ev)
		if len(missing) > 0 {
			f = f.Escalate(leak.High)
		}
		out = append(out, f)
	}
	if len(homes) > 0 {
		out = append(out, leak.LocalPaths.Find("Paths inside the archive start from a home folder and name the account that made it.", strings.Join(uniq(homes)[:min(len(uniq(homes)), 5)], "; ")))
	}
	return append(out, nested...), nil
}

func Reveal(b []byte, e Engine) ([]leak.Artifact, error) {
	zr, err := open(b)
	if err != nil {
		return nil, err
	}
	budget := int64(maxTotal)
	var out []leak.Artifact
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		data, err := read(f, &budget)
		if err != nil {
			continue
		}
		safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(f.Name)
		if path.Base(f.Name) == ".DS_Store" {
			if names := dsStoreNames(data); len(names) > 0 {
				out = append(out, leak.Artifact{Name: safe + ".names.txt", Note: "file names recorded by macOS for this folder", Data: []byte(strings.Join(names, "\n") + "\n")})
			}
			continue
		}
		if junk(f.Name) != "" {
			continue
		}
		arts, _ := e.Reveal(data)
		for _, a := range arts {
			out = append(out, leak.Artifact{Name: safe + "." + a.Name, Note: f.Name + ": " + a.Note, Data: a.Data})
		}
	}
	return out, nil
}

// Seal rewrites the archive without system files and with every member
// Sphinx can seal sealed. Entry names and order are kept.
func Seal(b []byte, e Engine) ([]byte, error) {
	zr, err := open(b)
	if err != nil {
		return nil, err
	}
	budget := int64(maxTotal)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if junk(f.Name) != "" {
			continue
		}
		hdr := &zip.FileHeader{Name: f.Name, Method: f.Method, Modified: f.Modified}
		hdr.SetMode(f.Mode())
		if f.FileInfo().IsDir() {
			if _, err := w.CreateHeader(hdr); err != nil {
				return nil, err
			}
			continue
		}
		data, err := read(f, &budget)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if sealed, err := e.Seal(data); err == nil {
			data = sealed
		}
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func uniq(vs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
