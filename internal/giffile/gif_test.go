package giffile

import (
	"bytes"
	"image/gif"
	"os"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

// testdata/comment.gif was written by ImageMagick with a comment block.
func TestCommentAndTrailer(t *testing.T) {
	b, err := os.ReadFile("testdata/comment.gif")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, "user=jroe"...)
	fs, err := Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, f := range fs {
		found[f.Rule] = f.Evidence
	}
	if found[leak.EmbeddedText.ID] != "Taken at grandma's, 14 rue des Tilleuls" || found[leak.TrailingData.ID] == "" {
		t.Fatalf("got %+v", fs)
	}
	sealed, err := Seal(b)
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := Inspect(sealed); len(left) != 0 {
		t.Fatalf("sealed copy still leaks: %+v", left)
	}
	if _, err := gif.DecodeAll(bytes.NewReader(sealed)); err != nil {
		t.Fatalf("sealed GIF does not decode: %v", err)
	}
}

func TestTruncated(t *testing.T) {
	b, _ := os.ReadFile("testdata/comment.gif")
	for i := 13; i < len(b); i += 97 {
		Inspect(b[:i])
		Seal(b[:i])
	}
}
