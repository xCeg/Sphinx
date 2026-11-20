package inflate

import (
	"bytes"
	"compress/flate"
	"math/rand"
	"testing"
)

func compress(t *testing.T, data []byte, level int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, level)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

func sample(n int) []byte {
	r := rand.New(rand.NewSource(1))
	words := []string{"sphinx ", "riddle ", "sand ", "gate ", "\n", "0123 ", "secret "}
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(words[r.Intn(len(words))])
		if r.Intn(10) == 0 {
			b.WriteByte(byte(r.Intn(256)))
		}
	}
	return b.Bytes()[:n]
}

func TestDecodeMatchesStdlib(t *testing.T) {
	for _, level := range []int{flate.NoCompression, flate.BestSpeed, flate.DefaultCompression, flate.BestCompression, flate.HuffmanOnly} {
		want := sample(200_000)
		out, err := Decode(compress(t, want, level), 0, 1<<24)
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		if !bytes.Equal(out.Data, want) {
			t.Fatalf("level %d: output differs", level)
		}
		for i, k := range out.Known {
			if !k {
				t.Fatalf("level %d: byte %d marked unknown in a full stream", level, i)
			}
		}
	}
}

func TestDecodeMidStreamMarksMissingHistory(t *testing.T) {
	want := sample(300_000)
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	w.Write(want[:150_000])
	w.Flush()
	boundary := buf.Len()
	w.Write(want[150_000:])
	w.Close()

	// A Flush ends with an empty stored block, so the next block starts on
	// the byte boundary recorded above.
	out, err := Decode(buf.Bytes(), boundary*8, 1<<24)
	if err != nil {
		t.Fatal(err)
	}
	tail := want[150_000:]
	if len(out.Data) != len(tail) {
		t.Fatalf("got %d bytes, want %d", len(out.Data), len(tail))
	}
	unknown := 0
	for i, k := range out.Known {
		if !k {
			unknown++
			continue
		}
		if out.Data[i] != tail[i] {
			t.Fatalf("known byte %d is wrong", i)
		}
	}
	if unknown == 0 {
		t.Fatal("expected references into the missing history to be flagged")
	}
}

func TestResyncFindsBlockBoundary(t *testing.T) {
	want := sample(400_000)
	full := compress(t, want, flate.BestCompression)
	cut := len(full) / 3
	out, start, ok := Resync(full[cut:], len(full)-cut, 1<<24)
	if !ok {
		t.Fatal("no block boundary found in the tail")
	}
	if start == 0 {
		t.Fatal("an arbitrary cut point is not expected to be a block boundary")
	}
	tail := want[len(want)-len(out.Data):]
	for i, k := range out.Known {
		if k && out.Data[i] != tail[i] {
			t.Fatalf("known byte %d is wrong", i)
		}
	}
}

func TestResyncRejectsGarbage(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	junk := make([]byte, 8192)
	r.Read(junk)
	if _, _, ok := Resync(junk, len(junk), 1<<20); ok {
		t.Fatal("random bytes resynchronised")
	}
}
