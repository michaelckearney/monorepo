package fixture

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
)

// screenshot is the attachment's file: a made-up screenshot of a sidebar,
// rows of text as bars, each project's New issue at its items' indent.
var screenshot = sync.OnceValue(func() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	fill := func(x, y, w, h int, c color.RGBA) {
		draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	page := color.RGBA{0xff, 0xff, 0xff, 0xff}
	sidebar := color.RGBA{0xfa, 0xfa, 0xfa, 0xff}
	border := color.RGBA{0xe4, 0xe4, 0xe7, 0xff}
	text := color.RGBA{0x3f, 0x3f, 0x46, 0xff}
	muted := color.RGBA{0xa1, 0xa1, 0xaa, 0xff}
	selected := color.RGBA{0xf4, 0xf4, 0xf5, 0xff}

	fill(0, 0, 640, 360, page)
	fill(0, 0, 220, 360, sidebar)
	fill(220, 0, 1, 360, border)
	fill(0, 48, 220, 1, border)
	fill(16, 18, 80, 12, text)
	y := 64
	for _, project := range []int{3, 2} {
		fill(16, y+4, 60, 8, muted)
		y += 24
		for i := 0; i < project; i++ {
			if i == 0 && project == 3 {
				fill(8, y-4, 204, 24, selected)
			}
			fill(24, y+2, 140-i*24, 10, text)
			y += 26
		}
		fill(24, y+2, 70, 10, muted)
		y += 36
	}
	fill(260, 24, 240, 16, text)
	for i := 0; i < 6; i++ {
		fill(260, 72+i*24, 340-(i%3)*60, 10, muted)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
})
