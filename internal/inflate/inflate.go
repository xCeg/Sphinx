// Package inflate decodes a raw DEFLATE stream that starts somewhere in the
// middle: the 32 KiB of history that earlier blocks would have provided is
// missing, so back-references into it produce bytes flagged as unknown.
//
// The design follows Mark Adler's puff.c: canonical Huffman decoding from
// code-length counts, which is slow per symbol but cheap to set up. That
// trade-off matters here because the caller tries millions of starting bit
// offsets and almost all of them fail within a few dozen bits.
package inflate

import "errors"

const (
	maxBits  = 15
	maxLCode = 286
	maxDCode = 30
	window   = 32768
)

var (
	errEOF     = errors.New("inflate: unexpected end of data")
	errInvalid = errors.New("inflate: invalid stream")
	errTooBig  = errors.New("inflate: output limit exceeded")
)

// Output is the decoded tail. Known[i] is false for bytes copied from the
// history the stream assumed but we never saw.
type Output struct {
	Data  []byte
	Known []bool
	// EndBit is the bit offset just past the final block.
	EndBit int
	// Distances counts back-reference distances. In image data the most
	// common distance is usually the length of a row.
	Distances map[int]int
}

type state struct {
	in     []byte
	bitPos int
	out    []byte
	known  []bool
	limit  int
	dists  map[int]int
}

func (s *state) bits(n int) (int, error) {
	if s.bitPos+n > len(s.in)*8 {
		return 0, errEOF
	}
	v := 0
	for i := 0; i < n; i++ {
		p := s.bitPos + i
		v |= int(s.in[p>>3]>>(p&7)&1) << i
	}
	s.bitPos += n
	return v, nil
}

type huffman struct {
	count  [maxBits + 1]uint16
	symbol []uint16
}

func (s *state) decode(h *huffman) (int, error) {
	code, first, index := 0, 0, 0
	for l := 1; l <= maxBits; l++ {
		b, err := s.bits(1)
		if err != nil {
			return 0, err
		}
		code |= b
		count := int(h.count[l])
		if code-count < first {
			return int(h.symbol[index+(code-first)]), nil
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	return 0, errInvalid
}

// construct returns the number of unused codes: 0 for a complete code,
// negative if over-subscribed.
func construct(h *huffman, lengths []uint8) int {
	h.count = [maxBits + 1]uint16{}
	for _, l := range lengths {
		h.count[l]++
	}
	if int(h.count[0]) == len(lengths) {
		h.symbol = h.symbol[:0]
		return 0
	}
	left := 1
	for l := 1; l <= maxBits; l++ {
		left <<= 1
		left -= int(h.count[l])
		if left < 0 {
			return left
		}
	}
	var offs [maxBits + 1]int
	for l := 1; l < maxBits; l++ {
		offs[l+1] = offs[l] + int(h.count[l])
	}
	if cap(h.symbol) < len(lengths) {
		h.symbol = make([]uint16, len(lengths))
	}
	h.symbol = h.symbol[:len(lengths)]
	for sym, l := range lengths {
		if l != 0 {
			h.symbol[offs[l]] = uint16(sym)
			offs[l]++
		}
	}
	return left
}

var (
	lenBase  = [29]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
	lenExtra = [29]int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	distBase = [30]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
	distExtr = [30]int{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
)

func (s *state) emit(b byte, known bool) error {
	if len(s.out) >= s.limit {
		return errTooBig
	}
	s.out = append(s.out, b)
	s.known = append(s.known, known)
	return nil
}

func (s *state) codes(lc, dc *huffman) error {
	for {
		sym, err := s.decode(lc)
		if err != nil {
			return err
		}
		switch {
		case sym < 256:
			if err := s.emit(byte(sym), true); err != nil {
				return err
			}
		case sym == 256:
			return nil
		default:
			sym -= 257
			if sym >= 29 {
				return errInvalid
			}
			extra, err := s.bits(lenExtra[sym])
			if err != nil {
				return err
			}
			n := lenBase[sym] + extra
			dsym, err := s.decode(dc)
			if err != nil {
				return err
			}
			if dsym >= 30 {
				return errInvalid
			}
			dextra, err := s.bits(distExtr[dsym])
			if err != nil {
				return err
			}
			dist := distBase[dsym] + dextra
			if dist > window {
				return errInvalid
			}
			s.dists[dist]++
			for range n {
				src := len(s.out) - dist
				var b byte
				known := false
				if src >= 0 {
					b, known = s.out[src], s.known[src]
				}
				if err := s.emit(b, known); err != nil {
					return err
				}
			}
		}
	}
}

func (s *state) stored() error {
	s.bitPos = (s.bitPos + 7) &^ 7
	p := s.bitPos >> 3
	if p+4 > len(s.in) {
		return errEOF
	}
	n := int(s.in[p]) | int(s.in[p+1])<<8
	nc := int(s.in[p+2]) | int(s.in[p+3])<<8
	if n != ^nc&0xffff {
		return errInvalid
	}
	p += 4
	if p+n > len(s.in) {
		return errEOF
	}
	for _, b := range s.in[p : p+n] {
		if err := s.emit(b, true); err != nil {
			return err
		}
	}
	s.bitPos = (p + n) * 8
	return nil
}

var fixedL, fixedD huffman

func init() {
	var l [288]uint8
	for i := range l {
		switch {
		case i < 144:
			l[i] = 8
		case i < 256:
			l[i] = 9
		case i < 280:
			l[i] = 7
		default:
			l[i] = 8
		}
	}
	construct(&fixedL, l[:])
	var d [30]uint8
	for i := range d {
		d[i] = 5
	}
	construct(&fixedD, d[:])
}

var clOrder = [19]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

func (s *state) dynamic(lc, dc *huffman) error {
	v, err := s.bits(14)
	if err != nil {
		return err
	}
	nlen, ndist, ncode := v&31+257, v>>5&31+1, v>>10+4
	if nlen > maxLCode || ndist > maxDCode {
		return errInvalid
	}
	var lengths [maxLCode + maxDCode]uint8
	for i := range ncode {
		b, err := s.bits(3)
		if err != nil {
			return err
		}
		lengths[clOrder[i]] = uint8(b)
	}
	var cl huffman
	if construct(&cl, lengths[:19]) != 0 {
		return errInvalid
	}
	for i := range lengths[:19] {
		lengths[i] = 0
	}
	for idx := 0; idx < nlen+ndist; {
		sym, err := s.decode(&cl)
		if err != nil {
			return err
		}
		if sym < 16 {
			lengths[idx] = uint8(sym)
			idx++
			continue
		}
		var fill uint8
		var rep int
		switch sym {
		case 16:
			if idx == 0 {
				return errInvalid
			}
			fill = lengths[idx-1]
			r, err := s.bits(2)
			if err != nil {
				return err
			}
			rep = 3 + r
		case 17:
			r, err := s.bits(3)
			if err != nil {
				return err
			}
			rep = 3 + r
		default:
			r, err := s.bits(7)
			if err != nil {
				return err
			}
			rep = 11 + r
		}
		if idx+rep > nlen+ndist {
			return errInvalid
		}
		for range rep {
			lengths[idx] = fill
			idx++
		}
	}
	if lengths[256] == 0 {
		return errInvalid
	}
	if left := construct(lc, lengths[:nlen]); left < 0 || (left > 0 && nlen-int(lc.count[0]) != 1) {
		return errInvalid
	}
	if left := construct(dc, lengths[nlen:nlen+ndist]); left < 0 || (left > 0 && ndist-int(dc.count[0]) != 1) {
		return errInvalid
	}
	return nil
}

// Decode inflates in starting at bit offset start until the final block,
// producing at most limit bytes.
func Decode(in []byte, start, limit int) (*Output, error) {
	s := &state{in: in, bitPos: start, limit: limit, dists: map[int]int{}}
	var lc, dc huffman
	for {
		last, err := s.bits(1)
		if err != nil {
			return nil, err
		}
		typ, err := s.bits(2)
		if err != nil {
			return nil, err
		}
		switch typ {
		case 0:
			err = s.stored()
		case 1:
			err = s.codes(&fixedL, &fixedD)
		case 2:
			if err = s.dynamic(&lc, &dc); err == nil {
				err = s.codes(&lc, &dc)
			}
		default:
			err = errInvalid
		}
		if err != nil {
			return nil, err
		}
		if last == 1 {
			return &Output{Data: s.out, Known: s.known, EndBit: s.bitPos, Distances: s.dists}, nil
		}
	}
}

// Resync finds the earliest bit offset from which in decodes cleanly and
// whose final block ends in the byte just before end. Requiring the stream
// to land exactly on a known boundary is what separates a real block start
// from the many offsets where random bits happen to parse for a while.
//
// Only dynamic-Huffman blocks are accepted as the first block. Fixed-code
// blocks accept nearly any bit pattern, so random bits would decode for
// thousands of symbols before failing, at every one of the millions of
// candidate offsets. A dynamic header must describe two complete prefix
// codes, which random bits almost never do, so most candidates die in a
// few dozen bits. Encoders emit fixed blocks only for short inputs, which
// a picture's worth of scanlines is not.
func Resync(in []byte, end, limit int) (*Output, int, bool) {
	in = in[:end]
	probe := &state{in: in}
	var lc, dc huffman
	for start := 0; start < end*8; start++ {
		probe.bitPos = start
		hdr, err := probe.bits(3)
		if err != nil {
			break
		}
		if hdr>>1 != 2 || probe.dynamic(&lc, &dc) != nil {
			continue
		}
		out, err := Decode(in, start, limit)
		if err == nil && (out.EndBit+7)/8 == end && len(out.Data) > 0 {
			return out, start, true
		}
	}
	return nil, 0, false
}
