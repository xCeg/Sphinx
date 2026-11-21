// Package c2pa recognises Content Credentials manifests (C2PA) embedded in
// images and pulls out the one field worth showing: which program made
// the claim. Verifying signatures is out of scope; the point is to tell
// people the history travels with the file.
package c2pa

import (
	"bytes"
)

// Generator returns the claim generator named in a JUMBF manifest store,
// or "" when the bytes hold none. The field is a CBOR text string keyed by
// "claim_generator" (C2PA 1.x) or "claim_generator_info"/"name" (2.x).
func Generator(b []byte) string {
	for _, key := range [][]byte{[]byte("claim_generator"), []byte("name")} {
		for p := 0; ; {
			i := bytes.Index(b[p:], key)
			if i < 0 {
				break
			}
			p += i + len(key)
			if s, ok := cborText(b[p:]); ok && printable(s) {
				return s
			}
		}
	}
	return ""
}

// Present reports whether b looks like a C2PA manifest store.
func Present(b []byte) bool {
	return bytes.Contains(b, []byte("c2pa")) && bytes.Contains(b, []byte("jumb"))
}

func cborText(b []byte) (string, bool) {
	if len(b) == 0 || b[0]>>5 != 3 {
		return "", false
	}
	n, h := int(b[0]&31), 1
	switch {
	case n == 24 && len(b) > 1:
		n, h = int(b[1]), 2
	case n == 25 && len(b) > 2:
		n, h = int(b[1])<<8|int(b[2]), 3
	case n > 23:
		return "", false
	}
	if n == 0 || h+n > len(b) {
		return "", false
	}
	return string(b[h : h+n]), true
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '�' {
			return false
		}
	}
	return len(s) >= 2
}
