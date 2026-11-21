package pdf

import (
	"fmt"
	"strings"
)

// Field is a filled-in form field.
type Field struct {
	Name, Value string
	Hidden      bool
}

// fields walks the AcroForm field tree and keeps fields that hold a value.
// Unchecked boxes ("Off") and signature fields carry nothing to report.
func (d *Document) fields() []Field {
	root := d.Root()
	if root == nil {
		return nil
	}
	var out []Field
	seen := map[Ref]bool{}
	var walk func(o Object, prefix string, ft Name, depth int)
	walk = func(o Object, prefix string, ft Name, depth int) {
		if r, ok := o.(Ref); ok {
			if seen[r] {
				return
			}
			seen[r] = true
		}
		f := d.Dict(o)
		if f == nil || depth > 32 || len(out) > 10000 {
			return
		}
		name := prefix
		if t := d.text(f["T"]); t != "" {
			if name != "" {
				name += "."
			}
			name += t
		}
		if t, ok := d.Resolve(f["FT"]).(Name); ok {
			ft = t
		}
		kids := d.list(f["Kids"])
		if v := d.fieldValue(f["V"]); v != "" && ft != "Sig" {
			out = append(out, Field{Name: name, Value: v, Hidden: d.widgetHidden(f, kids)})
		}
		for _, k := range kids {
			if kd := d.Dict(k); kd != nil && (kd["T"] != nil || kd["Kids"] != nil) {
				walk(k, name, ft, depth+1)
			}
		}
	}
	for _, f := range d.list(d.Dict(root["AcroForm"])["Fields"]) {
		walk(f, "", "", 0)
	}
	return out
}

func (d *Document) fieldValue(o Object) string {
	switch v := d.Resolve(o).(type) {
	case String:
		return d.text(v)
	case Name:
		if v == "Off" || v == "" {
			return ""
		}
		return string(v)
	case Array:
		var parts []string
		for _, x := range v {
			if s := d.fieldValue(x); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// widgetHidden reports a field whose every widget is hidden, not shown on
// screen, or zero-sized: its value travels with the file but is never seen.
func (d *Document) widgetHidden(f Dict, kids Array) bool {
	widgets := []Dict{f}
	if f["Rect"] == nil {
		widgets = nil
		for _, k := range kids {
			if kd := d.Dict(k); kd != nil && kd["T"] == nil {
				widgets = append(widgets, kd)
			}
		}
	}
	if len(widgets) == 0 {
		return true
	}
	for _, w := range widgets {
		flags, _ := d.Num(w["F"])
		r, ok := d.rect(w["Rect"])
		visible := int(flags)&(2|32) == 0 && ok && r[2]-r[0] > 0.5 && r[3]-r[1] > 0.5
		if visible {
			return false
		}
	}
	return true
}

func formSummary(fs []Field) (string, string) {
	hidden := 0
	var samples []string
	for _, f := range fs {
		if f.Hidden {
			hidden++
		}
	}
	for _, f := range fs {
		if len(samples) == 6 {
			break
		}
		label := f.Name
		if f.Hidden {
			label += " (hidden)"
		}
		samples = append(samples, fmt.Sprintf("%s = %q", label, truncate(f.Value, 40)))
	}
	msg := fmt.Sprintf("%s of the form hold%s values.", count(len(fs), "field"), verbS(len(fs)))
	if hidden > 0 {
		msg = fmt.Sprintf("%s of the form hold%s values, %d of them in fields that are never shown.", count(len(fs), "field"), verbS(len(fs)), hidden)
	}
	if len(fs) > 6 {
		samples = append(samples, fmt.Sprintf("and %d more", len(fs)-6))
	}
	return msg, strings.Join(samples, "; ")
}
