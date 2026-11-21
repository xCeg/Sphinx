// Package tifffile inspects and seals TIFF images and the camera raw
// formats built on TIFF (DNG and most others), whose metadata lives in the
// same directories as the image.
package tifffile

import (
	"errors"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/photo"
	"github.com/xceg/sphinx/internal/xmp"
)

func Inspect(b []byte) ([]leak.Finding, error) {
	m, err := exif.Parse(b)
	if err != nil {
		return nil, errors.New("damaged TIFF structure")
	}
	return photo.Findings(m, xmp.Parse(b)), nil
}

// Seal blanks identifying tags in place; see exif.Blank.
func Seal(b []byte) ([]byte, error) {
	out := append([]byte(nil), b...)
	if err := exif.Blank(out); err != nil {
		return nil, err
	}
	return out, nil
}
