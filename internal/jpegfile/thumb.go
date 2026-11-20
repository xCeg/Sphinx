package jpegfile

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
)

const grid = 32

// signature reduces an image to a grid x grid luminance map after trimming
// the black bars some cameras use to letterbox a 4:3 preview of a 3:2 photo.
func signature(img image.Image) ([]float64, float64) {
	return signatureOf(img, trimBars(img))
}

// centreCrop is the largest centred rectangle of b with the given aspect.
func centreCrop(b image.Rectangle, aspect float64) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	if float64(w)/float64(h) > aspect {
		nw := int(float64(h) * aspect)
		return image.Rect(b.Min.X+(w-nw)/2, b.Min.Y, b.Min.X+(w-nw)/2+nw, b.Max.Y)
	}
	nh := int(float64(w) / aspect)
	return image.Rect(b.Min.X, b.Min.Y+(h-nh)/2, b.Max.X, b.Min.Y+(h-nh)/2+nh)
}

func signatureOf(img image.Image, r image.Rectangle) ([]float64, float64) {
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

// Thresholds measured on the calibration corpus (see the README): the
// previews of 32 unedited camera photos stay within 0.18 standard
// deviations of their photo, a camera that cuts the sides of its preview
// scores 0.41 against the matching crop, and the stale preview of a
// photo cropped after the fact scores 0.83.
const (
	maxDistance  = 0.45
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
	diff := distance(ms, ts)
	gap := ma/ta - 1
	if gap < 0 {
		gap = -gap
	}
	if gap > maxAspectGap {
		// Some cameras make a 4:3 preview of a 3:2 photo by cutting its
		// sides. Such a preview shows less than the photo, not more.
		inner, _ := signatureOf(mi, centreCrop(trimBars(mi), ta))
		if distance(inner, ts) <= maxDistance {
			return sameImage, true
		}
		if diff > maxDistance/2 {
			return cropped, true
		}
	}
	if diff > maxDistance {
		return edited, true
	}
	return sameImage, true
}

// distance compares two signatures after normalising each to zero mean and
// unit spread, so that a preview rendered brighter or with more contrast
// than the photo still counts as the same picture. The result is in units
// of standard deviation.
func distance(a, b []float64) float64 {
	a, b = normalise(a), normalise(b)
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum / float64(len(a))
}

func normalise(v []float64) []float64 {
	var mean, sd float64
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	for _, x := range v {
		sd += (x - mean) * (x - mean)
	}
	sd = math.Sqrt(sd / float64(len(v)))
	if sd < 1 {
		sd = 1
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = (x - mean) / sd
	}
	return out
}
