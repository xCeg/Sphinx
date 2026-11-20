// Package pdf reads just enough of PDF to answer one question: what does a
// reader of this file get that a viewer of it does not? It locates objects
// by scanning rather than trusting the cross-reference table, because the
// files most worth checking (hastily redacted, incrementally edited) are
// often the ones with stale or broken tables.
package pdf

import "fmt"

type (
	Object any
	Name   string
	Dict   map[Name]Object
	Array  []Object
	String []byte
	Ref    struct{ Num, Gen int }
	// Keyword is a bare operator or token such as obj, R or Tj.
	Keyword string
)

type Stream struct {
	Dict Dict
	Raw  []byte
}

func (r Ref) String() string { return fmt.Sprintf("%d %d R", r.Num, r.Gen) }

func num(o Object) (float64, bool) {
	switch v := o.(type) {
	case int:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

func (d Dict) Name(k Name) Name {
	n, _ := d[k].(Name)
	return n
}
