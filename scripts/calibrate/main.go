// Command calibrate runs Sphinx over a corpus and prints the tables used in
// the README: files per format, failures, findings per rule, and timing.
//
//	scripts/fetch-corpus.sh /tmp/corpus
//	go run ./scripts/calibrate /tmp/corpus
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/xceg/sphinx"
)

type tally struct {
	files, failed, flagged int
	bytes                  int64
	elapsed                time.Duration
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: calibrate DIR...")
		os.Exit(2)
	}
	formats := map[string]*tally{}
	rules := map[string]map[string]int{}
	var total time.Duration
	for _, root := range os.Args[1:] {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			f := sphinx.Format(data)
			if f == "" {
				return nil
			}
			t := formats[f]
			if t == nil {
				t = &tally{}
				formats[f] = t
			}
			start := time.Now()
			findings, err := sphinx.Inspect(data)
			el := time.Since(start)
			total += el
			t.files++
			t.bytes += int64(len(data))
			t.elapsed += el
			if err != nil {
				t.failed++
				return nil
			}
			seen := map[string]bool{}
			for _, x := range findings {
				if x.Severity >= sphinx.Medium {
					t.flagged++
					break
				}
			}
			for _, x := range findings {
				key := x.Rule + " " + x.Severity.String()
				if seen[key] {
					continue
				}
				seen[key] = true
				if rules[key] == nil {
					rules[key] = map[string]int{}
				}
				rules[key][f]++
			}
			return nil
		})
	}

	names := sortedKeys(formats)
	fmt.Println("| Format | Files | Size | Unreadable | Medium or worse | Time |")
	fmt.Println("|---|---:|---:|---:|---:|---:|")
	var files int
	for _, n := range names {
		t := formats[n]
		files += t.files
		fmt.Printf("| %s | %d | %.0f MB | %d | %d | %.2f s |\n", n, t.files, float64(t.bytes)/(1<<20), t.failed, t.flagged, t.elapsed.Seconds())
	}
	fmt.Printf("\n%d files, %.2f s of inspection on one core.\n\n", files, total.Seconds())
	fmt.Println("| Rule | Severity | Files |")
	fmt.Println("|---|---|---|")
	for _, k := range sortedKeys(rules) {
		var parts string
		for _, f := range sortedKeys(rules[k]) {
			if parts != "" {
				parts += ", "
			}
			parts += fmt.Sprintf("%s %d", f, rules[k][f])
		}
		var id, sev string
		fmt.Sscan(k, &id, &sev)
		fmt.Printf("| %s %s | %s | %s |\n", id, sphinx.LookupRule(id).Name, sev, parts)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
