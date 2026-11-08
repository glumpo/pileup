package thumbhash_test

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"testing"

	"github.com/glumpo/pileup/core/thumbhash"
)

func draw(w, h int, f func(x, y int) color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, f(x, y))
		}
	}
	return img
}

func landscape(x, y int) color.RGBA {
	return color.RGBA{uint8(x * 3), uint8(y * 4), uint8(x * y % 256), 255}
}

func TestMatchesReferenceImplementation(t *testing.T) {
	for name, c := range map[string]struct {
		img  image.Image
		want string
	}{
		"landscape": {draw(80, 60, landscape), "dd070a2d987076777077777777687777909009f788"},
		"portrait": {draw(40, 100, func(x, y int) color.RGBA {
			return color.RGBA{uint8((x*7 + y*3) % 256), uint8(x * y % 256), uint8(255 - y*2), 255}
		}), "e116065b04558f65446886777780b7b99f"},
		"square": {draw(100, 100, func(x, y int) color.RGBA {
			c := color.RGBA{20, 40, uint8((x + y) % 256), 255}
			if x < 50 {
				c.R = 230
			}
			if y < 50 {
				c.G = 200
			}
			return c
		}), "9c080a6f228f848a8c788877784887888789f8e7777f7207"},
		"tiny": {draw(3, 2, func(x, y int) color.RGBA {
			return color.RGBA{uint8(x * 100), uint8(y * 200), 128, 255}
		}), "1b07364da27678097688788788988890717017f788"},
	} {
		if got := hex.EncodeToString(thumbhash.Encode(c.img)); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func TestLargeImageIsReducedToFitHundredPixels(t *testing.T) {
	small := draw(100, 75, landscape)
	large := draw(300, 225, func(x, y int) color.RGBA { return landscape(x/3, y/3) })
	if got, want := thumbhash.Encode(large), thumbhash.Encode(small); !bytes.Equal(got, want) {
		t.Fatalf("300x225 image: %x, want the hash of the same picture at 100x75 %x", got, want)
	}
	shifted := draw(110, 85, func(x, y int) color.RGBA { return landscape(x-10, y-10) }).SubImage(image.Rect(10, 10, 110, 85))
	if got, want := thumbhash.Encode(shifted), thumbhash.Encode(small); !bytes.Equal(got, want) {
		t.Fatalf("sub-image: %x, want %x", got, want)
	}
}
