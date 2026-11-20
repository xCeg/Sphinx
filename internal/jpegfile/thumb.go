package jpegfile

import (
	"bytes"
	"image"
	"image/jpeg"
)

const grid = 32

// signature reduces an image to a grid x grid luminance map after trimming
// the black bars some cameras use to letterbox a 4:3 preview of a 3:2 photo.
func signature(img image.Image) ([]float64, float64) {
	r := trimBars(img)
	out := make([]float64, grid*grid)
	w, h := r.Dx(), r.Dy()
	for gy := range grid {
		for gx := range grid {
			x0, x1 := r.Min.X+gx*w/grid, r.Min.X+(gx+1)*w/grid
			y0, y1 := r.Min.Y+gy*h/grid, r.Min.Y+(gy+1)*h/grid
			var sum, n float64
			for y := y0; y < max(y1, y0+1); y++ {
				for x := x0; x < max(x1, x0+1); x += max(1, (x1-x0)/8) {
					sum += lum(img, x, y)
					n++
				}
			}
			out[gy*grid+gx] = sum / n
		}
	}
	return out, float64(w) / float64(h)
}

func lum(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
}

func trimBars(img image.Image) image.Rectangle {
	b := img.Bounds()
	dark := func(x0, y0, x1, y1 int) bool {
		var sum, n float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				sum += lum(img, x, y)
				n++
			}
		}
		return n > 0 && sum/n < 12
	}
	r := b
	for r.Dy() > 8 && dark(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1) {
		r.Min.Y++
	}
	for r.Dy() > 8 && dark(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y) {
		r.Max.Y--
	}
	for r.Dx() > 8 && dark(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y) {
		r.Min.X++
	}
	for r.Dx() > 8 && dark(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y) {
		r.Max.X--
	}
	if r.Dx()*r.Dy()*4 < b.Dx()*b.Dy() {
		return b
	}
	return r
}

type verdict int

const (
	sameImage verdict = iota
	cropped
	edited
)

// Thresholds measured on the calibration corpus (see the README): genuine
// previews of the same photo stay under 12 grey levels of mean difference,
// and an aspect-ratio change of more than 5% does not happen without a crop.
const (
	maxLumDiff   = 20.0
	maxAspectGap = 0.05
)

func compareThumb(main, thumb []byte) (verdict, bool) {
	mi, err := jpeg.Decode(bytes.NewReader(main))
	if err != nil {
		return sameImage, false
	}
	ti, err := jpeg.Decode(bytes.NewReader(thumb))
	if err != nil || ti.Bounds().Dx() < 16 || ti.Bounds().Dy() < 16 {
		return sameImage, false
	}
	ms, ma := signature(mi)
	ts, ta := signature(ti)
	var diff float64
	for i := range ms {
		d := ms[i] - ts[i]
		if d < 0 {
			d = -d
		}
		diff += d
	}
	diff /= float64(len(ms))
	gap := ma/ta - 1
	if gap < 0 {
		gap = -gap
	}
	switch {
	case gap > maxAspectGap && diff > maxLumDiff/2:
		return cropped, true
	case diff > maxLumDiff:
		return edited, true
	}
	return sameImage, true
}
