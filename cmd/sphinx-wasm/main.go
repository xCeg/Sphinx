//go:build js && wasm

// Command sphinx-wasm exposes Sphinx to the website, which runs it inside
// the visitor's browser: the file being checked never leaves the machine.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/xceg/sphinx"
)

type finding struct {
	sphinx.Finding
	Title string `json:"title"`
	Why   string `json:"why"`
	Fix   string `json:"fix"`
}

func bytesOf(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

func toJS(b []byte) js.Value {
	a := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(a, b)
	return a
}

func errorResult(err error) any {
	return map[string]any{"error": err.Error()}
}

func inspect(_ js.Value, args []js.Value) any {
	data := bytesOf(args[0])
	fs, err := sphinx.Inspect(data)
	if err != nil {
		return errorResult(err)
	}
	out := make([]finding, len(fs))
	for i, f := range fs {
		r := sphinx.LookupRule(f.Rule)
		out[i] = finding{f, r.Title, r.Why, r.Fix}
	}
	b, _ := json.Marshal(map[string]any{
		"format": sphinx.Format(data), "verdict": sphinx.VerdictOf(fs), "findings": out,
	})
	return string(b)
}

func reveal(_ js.Value, args []js.Value) any {
	arts, err := sphinx.Reveal(bytesOf(args[0]))
	if err != nil {
		return errorResult(err)
	}
	list := make([]any, len(arts))
	for i, a := range arts {
		list[i] = map[string]any{"name": a.Name, "note": a.Note, "data": toJS(a.Data)}
	}
	return list
}

func seal(_ js.Value, args []js.Value) any {
	out, err := sphinx.Seal(bytesOf(args[0]))
	if err != nil {
		return errorResult(err)
	}
	return toJS(out)
}

func rules(js.Value, []js.Value) any {
	type rule struct {
		ID, Name, Severity, Title, Why, Fix string
		Formats                             []string
	}
	var out []rule
	for _, r := range sphinx.Rules() {
		out = append(out, rule{r.ID, r.Name, r.Severity.String(), r.Title, r.Why, r.Fix, r.Formats})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func main() {
	js.Global().Set("sphinx", js.ValueOf(map[string]any{
		"inspect": js.FuncOf(inspect),
		"reveal":  js.FuncOf(reveal),
		"seal":    js.FuncOf(seal),
		"rules":   js.FuncOf(rules),
	}))
	select {}
}
