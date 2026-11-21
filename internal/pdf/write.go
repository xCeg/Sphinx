package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// Seal rewrites the document as a single revision holding only the objects
// reachable from its catalog. Earlier revisions, objects that updates
// orphaned, the document information dictionary and XMP metadata are left
// behind. Page content is copied untouched: text under a box stays under
// the box, and the post-seal report says so.
func Seal(b []byte) ([]byte, error) {
	f, err := Parse(b)
	if err != nil {
		return nil, err
	}
	d := f.Final()
	if d.trailer["Encrypt"] != nil {
		return nil, errors.New("encrypted PDFs are not sealed: rewriting one would remove its protection")
	}
	rootRef, ok := d.trailer["Root"].(Ref)
	pages := len(d.Pages())
	if !ok || d.Root() == nil || pages == 0 {
		return nil, errors.New("the PDF is too damaged to rewrite safely")
	}
	root := Dict{}
	for k, v := range d.Root() {
		if k != "Metadata" && k != "PieceInfo" {
			root[k] = v
		}
	}
	if pages := d.pagesRoot(); pages != nil {
		root["Pages"] = pages
	}

	keep := map[int]Object{rootRef.Num: root}
	var visit func(o Object, depth int)
	visit = func(o Object, depth int) {
		if depth > 256 {
			return
		}
		switch v := o.(type) {
		case Ref:
			if _, done := keep[v.Num]; done {
				return
			}
			obj, ok := d.objects[v.Num]
			if !ok {
				return
			}
			keep[v.Num] = obj
			visit(obj, depth+1)
		case Dict:
			for k, x := range v {
				if k != "Metadata" && k != "PieceInfo" {
					visit(x, depth+1)
				}
			}
		case Array:
			for _, x := range v {
				visit(x, depth+1)
			}
		case *Stream:
			visit(v.Dict, depth+1)
		}
	}
	visit(root, 0)

	nums := make([]int, 0, len(keep))
	for n := range keep {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := map[int]int{}
	for _, n := range nums {
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n", n)
		writeObject(&out, scrub(keep[n]))
		out.WriteString("\nendobj\n")
	}
	size := nums[len(nums)-1] + 1
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", size)
	for n := 1; n < size; n++ {
		if off, ok := offsets[n]; ok {
			fmt.Fprintf(&out, "%010d 00000 n \n", off)
		} else {
			out.WriteString("0000000000 65535 f \n")
		}
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, rootRef.Num, xref)
	check, err := Parse(out.Bytes())
	if err != nil || len(check.Final().Pages()) != pages {
		return nil, errors.New("the rewritten PDF lost pages; the original is left as it is")
	}
	return out.Bytes(), nil
}

// scrub drops per-object metadata streams and application data, which can
// hold the same names and paths as the document information.
func scrub(o Object) Object {
	switch v := o.(type) {
	case Dict:
		if v["Metadata"] == nil && v["PieceInfo"] == nil {
			return v
		}
		out := Dict{}
		for k, x := range v {
			if k != "Metadata" && k != "PieceInfo" {
				out[k] = x
			}
		}
		return out
	case *Stream:
		return &Stream{Dict: scrub(v.Dict).(Dict), Raw: v.Raw}
	}
	return o
}

func writeObject(w *bytes.Buffer, o Object) {
	switch v := o.(type) {
	case nil:
		w.WriteString("null")
	case bool:
		w.WriteString(strconv.FormatBool(v))
	case int:
		w.WriteString(strconv.Itoa(v))
	case float64:
		w.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	case Name:
		writeName(w, v)
	case String:
		fmt.Fprintf(w, "<%x>", []byte(v))
	case Ref:
		// Every object is written as generation 0, so references follow.
		fmt.Fprintf(w, "%d 0 R", v.Num)
	case Array:
		w.WriteByte('[')
		for i, x := range v {
			if i > 0 {
				w.WriteByte(' ')
			}
			writeObject(w, x)
		}
		w.WriteByte(']')
	case Dict:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		w.WriteString("<<")
		for _, k := range keys {
			w.WriteByte(' ')
			writeName(w, Name(k))
			w.WriteByte(' ')
			writeObject(w, v[Name(k)])
		}
		w.WriteString(" >>")
	case *Stream:
		dict := Dict{}
		for k, x := range v.Dict {
			dict[k] = x
		}
		dict["Length"] = len(v.Raw)
		writeObject(w, dict)
		w.WriteString("\nstream\n")
		w.Write(v.Raw)
		w.WriteString("\nendstream")
	default:
		w.WriteString("null")
	}
}

func writeName(w *bytes.Buffer, n Name) {
	w.WriteByte('/')
	for _, c := range []byte(n) {
		if c < 0x21 || c > 0x7e || c == '#' || isDelim(c) {
			fmt.Fprintf(w, "#%02X", c)
			continue
		}
		w.WriteByte(c)
	}
}
