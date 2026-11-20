package pngfile

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"sort"

	"github.com/xceg/sphinx/internal/inflate"
)

// zlibTail reassembles the end of the original image's compressed stream
// from what follows IEND: a headless fragment of one IDAT chunk, then
// zero or more whole IDAT chunks, then the original IEND. Whole chunks are
// only trusted when their CRC checks out, which rules out chance matches of
// the bytes "IDAT" inside compressed data.
func zlibTail(t []byte) []byte {
	var out []byte
	fragmentEnd := -1
	p := 0
	for p+12 <= len(t) {
		i := bytes.Index(t[p:], []byte("IDAT"))
		if i < 0 {
			break
		}
		at := p + i - 4
		if at < 0 {
			p += i + 4
			continue
		}
		n := int(binary.BigEndian.Uint32(t[at:]))
		if n < 0 || !validCRC(t, at, n) {
			p += i + 4
			continue
		}
		if fragmentEnd < 0 {
			fragmentEnd = at - 4
			if fragmentEnd > 0 {
				out = append(out, t[:fragmentEnd]...)
			}
		}
		out = append(out, t[at+8:at+8+n]...)
		p = at + 12 + n
	}
	if fragmentEnd < 0 {
		iend := bytes.LastIndex(t, []byte("IEND"))
		if iend < 8 {
			return nil
		}
		out = append(out, t[:iend-8]...)
	}
	return out
}

// Recovered is the part of the original picture found after IEND.
type Recovered struct {
	Image         *image.NRGBA
	Width, Rows   int
	UnknownPixels int
}

const maxRecovered = 64 << 20

func recoverCrop(h header, trailing []byte) (*Recovered, bool) {
	stream := zlibTail(trailing)
	if len(stream) < 64 {
		return nil, false
	}
	out, _, ok := inflate.Resync(stream, len(stream)-4, maxRecovered)
	if !ok {
		return nil, false
	}
	geo, ok := guessGeometry(out.Data, out.Known, out.Distances, h.bytesPerPixel())
	if !ok {
		return nil, false
	}
	img, unknown := unfilter(out.Data, out.Known, geo)
	return &Recovered{Image: img, Width: geo.width, Rows: geo.rows, UnknownPixels: unknown}, true
}

type geometry struct {
	stride, bpp, width, rows int
}

const minRows = 8

// guessGeometry recovers the original row width without being told.
//
// Decoded data ends exactly on a row boundary, so stepping back from the
// end by the true stride lands on filter-type bytes (0 to 4) every time.
// On screenshots that test alone is weak, because filtered flat colour is
// mostly zero bytes and many wrong strides pass too. The compressor breaks
// the tie: PNG rows resemble the row above, so a back-reference distance
// equal to the stride is far more common than any unrelated distance.
// The best-supported survivors are unfiltered and ranked by smoothness. A
// whole multiple of the stride can look as smooth as the truth on a
// repetitive screen, so a divisor of the winner that is nearly as smooth
// takes its place.
func guessGeometry(data []byte, known []bool, dists map[int]int, hintBPP int) (geometry, bool) {
	n := len(data)
	var strides []int
	for stride := 2; stride <= n/minRows && stride <= maxStride; stride++ {
		if filterBytesAligned(data, known, stride) {
			strides = append(strides, stride)
		}
	}
	sort.SliceStable(strides, func(i, j int) bool { return dists[strides[i]] > dists[strides[j]] })
	strides = strides[:min(len(strides), 8)]

	type scored struct {
		g    geometry
		cost float64
	}
	var cands []scored
	for _, stride := range strides {
		for _, bpp := range candidatesBPP(hintBPP) {
			if (stride-1)%bpp == 0 {
				g := geometry{stride: stride, bpp: bpp, width: (stride - 1) / bpp, rows: n / stride}
				cands = append(cands, scored{g, roughness(data, known, g)})
			}
		}
	}
	if len(cands) == 0 {
		return geometry{}, false
	}
	best := cands[0]
	for _, c := range cands {
		if c.cost < best.cost {
			best = c
		}
	}
	for _, c := range cands {
		if best.g.stride%c.g.stride == 0 && c.g.stride < best.g.stride && c.cost <= best.cost*1.5+0.5 {
			best = c
		}
	}
	return best.g, true
}

const maxStride = 1 + 8*16384

func filterBytesAligned(data []byte, known []bool, stride int) bool {
	n := len(data)
	hits, seen := 0, 0
	for k := 1; k <= n/stride; k++ {
		i := n - k*stride
		if !known[i] {
			continue
		}
		seen++
		if data[i] <= 4 {
			hits++
		}
	}
	return seen >= minRows/2 && hits*100 >= seen*97
}

func candidatesBPP(hint int) []int {
	out := []int{hint}
	for _, b := range []int{4, 3} {
		if b != hint {
			out = append(out, b)
		}
	}
	return out
}

// roughness is the mean difference between vertically adjacent bytes,
// counted only where both came from data we actually decoded: bytes that
// referenced the lost history are zero and would make any guess look smooth.
func roughness(data []byte, known []bool, g geometry) float64 {
	rows := min(g.rows, 256)
	start := len(data) - rows*g.stride
	prev := make([]byte, g.stride-1)
	cur := make([]byte, g.stride-1)
	var sum, count float64
	for r := 0; r < rows; r++ {
		off := start + r*g.stride
		line := data[off : off+g.stride]
		defilter(line[0], line[1:], cur, prev, g.bpp)
		if r > 0 {
			for i := range cur {
				if known[off+1+i] && known[off+1+i-g.stride] {
					sum += float64(abs(int(cur[i]) - int(prev[i])))
					count++
				}
			}
		}
		prev, cur = cur, prev
	}
	if count == 0 {
		return 255
	}
	return sum / count
}

func defilter(ft byte, in, cur, prev []byte, bpp int) {
	for i, x := range in {
		var a, b, c byte
		if i >= bpp {
			a, c = cur[i-bpp], prev[i-bpp]
		}
		b = prev[i]
		switch ft {
		case 1:
			x += a
		case 2:
			x += b
		case 3:
			x += byte((int(a) + int(b)) / 2)
		case 4:
			x += paeth(a, b, c)
		}
		cur[i] = x
	}
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// unfilter renders the recovered rows. Bytes that referenced the lost
// history are decoded as zero; the damage they cause spreads down through
// Up and Paeth filters as colour drift, not as gaps, which keeps text legible.
// Alpha is forced opaque for the same reason: a drifted alpha channel would
// fade otherwise readable pixels to nothing.
func unfilter(data []byte, known []bool, g geometry) (*image.NRGBA, int) {
	start := len(data) - g.rows*g.stride
	img := image.NewNRGBA(image.Rect(0, 0, g.width, g.rows))
	prev := make([]byte, g.stride-1)
	cur := make([]byte, g.stride-1)
	unknown := 0
	for r := 0; r < g.rows; r++ {
		off := start + r*g.stride
		line := data[off : off+g.stride]
		defilter(line[0], line[1:], cur, prev, g.bpp)
		for x := 0; x < g.width; x++ {
			px := cur[x*g.bpp:]
			var c color.NRGBA
			switch g.bpp {
			case 4, 3:
				c = color.NRGBA{px[0], px[1], px[2], 255}
			default:
				c = color.NRGBA{px[0], px[0], px[0], 255}
			}
			if !known[off+1+x*g.bpp] {
				unknown++
			}
			img.SetNRGBA(x, r, c)
		}
		prev, cur = cur, prev
	}
	return img, unknown
}
