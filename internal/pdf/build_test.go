package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"sort"
	"strings"
)

// builder writes small PDFs by hand, so each test controls exactly which
// operators and objects appear.
type builder struct {
	objs map[int]string
	next int
}

func newBuilder() *builder {
	b := &builder{objs: map[int]string{}, next: 10}
	b.objs[3] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
	return b
}

func (b *builder) add(body string) int {
	n := b.next
	b.next++
	b.objs[n] = body
	return n
}

func stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

func deflated(data string) string {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte(data))
	w.Close()
	return fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", z.Len(), z.String())
}

// page adds a page with the given content stream and extra dictionary
// entries, and returns its object number.
func (b *builder) page(content, extra string) int {
	c := b.add(stream("", content))
	return b.add(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> /ExtGState << /Half << /ca 0.3 >> /Mul << /BM /Multiply >> >> >> %s >>", c, extra))
}

func (b *builder) bytes(pages []int, trailer string) []byte {
	var kids []string
	for _, p := range pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", p))
	}
	if _, ok := b.objs[1]; !ok {
		b.objs[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	}
	b.objs[2] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	var nums []int
	for n := range b.objs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, b.objs[n])
	}
	fmt.Fprintf(&out, "trailer\n<< /Root 1 0 R %s >>\nstartxref\n0\n%%%%EOF\n", trailer)
	return out.Bytes()
}

// update appends an incremental update that redefines objects.
func update(base []byte, objs map[int]string) []byte {
	out := bytes.NewBuffer(append([]byte(nil), base...))
	for n, body := range objs {
		fmt.Fprintf(out, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	out.WriteString("trailer\n<< /Root 1 0 R >>\nstartxref\n0\n%%EOF\n")
	return out.Bytes()
}

func text(x, y int, s string) string {
	return fmt.Sprintf("BT /F1 12 Tf %d %d Td (%s) Tj ET\n", x, y, s)
}
