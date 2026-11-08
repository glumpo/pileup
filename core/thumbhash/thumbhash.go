package thumbhash

import (
	"image"
	"image/draw"
	"math"
)

const maxSide = 100

func Encode(img image.Image) []byte {
	w, h, rgb := pixels(img)
	m := float64(max(w, h))
	lx := max(1, int(round(7*float64(w)/m)))
	ly := max(1, int(round(7*float64(h)/m)))
	l := make([]float64, w*h)
	p := make([]float64, w*h)
	q := make([]float64, w*h)
	for i := range l {
		r := 1.0 / 255 * rgb[3*i]
		g := 1.0 / 255 * rgb[3*i+1]
		b := 1.0 / 255 * rgb[3*i+2]
		l[i] = (r + g + b) / 3
		p[i] = (r+g)/2 - b
		q[i] = r - g
	}

	lDC, lAC, lScale := channel(l, w, h, max(3, lx), max(3, ly))
	pDC, pAC, pScale := channel(p, w, h, 3, 3)
	qDC, qAC, qScale := channel(q, w, h, 3, 3)

	landscape := 0
	side := lx
	if w > h {
		landscape, side = 1, ly
	}
	header24 := int(round(63*lDC)) | int(round(31.5+31.5*pDC))<<6 | int(round(31.5+31.5*qDC))<<12 | int(round(31*lScale))<<18
	header16 := side | int(round(63*pScale))<<3 | int(round(63*qScale))<<9 | landscape<<15
	ac := append(append(lAC, pAC...), qAC...)
	hash := make([]byte, 5+(len(ac)+1)/2)
	hash[0], hash[1], hash[2] = byte(header24), byte(header24>>8), byte(header24>>16)
	hash[3], hash[4] = byte(header16), byte(header16>>8)
	for i, f := range ac {
		hash[5+i/2] |= byte(int(round(15*f)) << ((i & 1) << 2))
	}
	return hash
}

func channel(c []float64, w, h, nx, ny int) (dc float64, ac []float64, scale float64) {
	fx := make([]float64, w)
	for cy := range ny {
		for cx := 0; cx*ny < nx*(ny-cy); cx++ {
			var f float64
			for x := range w {
				fx[x] = math.Cos(math.Pi / float64(w) * float64(cx) * (float64(x) + 0.5))
			}
			for y := range h {
				fy := math.Cos(math.Pi / float64(h) * float64(cy) * (float64(y) + 0.5))
				for x := range w {
					f += c[x+y*w] * fx[x] * fy
				}
			}
			f /= float64(w * h)
			if cx > 0 || cy > 0 {
				ac = append(ac, f)
				scale = max(scale, math.Abs(f))
			} else {
				dc = f
			}
		}
	}
	if scale > 0 {
		for i := range ac {
			ac[i] = 0.5 + 0.5/scale*ac[i]
		}
	}
	return dc, ac, scale
}

func pixels(img image.Image) (int, int, []float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := w, h
	if m := max(w, h); m > maxSide {
		tw, th = max(1, w*maxSide/m), max(1, h*maxSide/m)
	}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	rgb := make([]float64, 0, 3*tw*th)
	for ty := range th {
		for tx := range tw {
			var r, g, bl, k float64
			for y := ty * h / th; y < (ty+1)*h/th; y++ {
				for x := tx * w / tw; x < (tx+1)*w/tw; x++ {
					c := src.Pix[y*src.Stride+4*x:]
					r, g, bl, k = r+float64(c[0]), g+float64(c[1]), bl+float64(c[2]), k+1
				}
			}
			rgb = append(rgb, r/k, g/k, bl/k)
		}
	}
	return tw, th, rgb
}

func round(x float64) float64 {
	return math.Floor(x + 0.5)
}
