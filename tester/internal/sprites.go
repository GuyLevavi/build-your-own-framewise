package internal

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"
)

// The people and cars in the scenes: cut out of photos (COCO and Ultralytics'
// car-parts dataset) with a segmentation model, kept with their alpha mask.
//
//go:embed assets/*.png
var assets embed.FS

var (
	spritesMu sync.Mutex
	originals = map[string]*image.NRGBA{}
	scaled    = map[string]*image.NRGBA{}
)

func original(name string) *image.NRGBA {
	if img, ok := originals[name]; ok {
		return img
	}
	data, err := assets.ReadFile("assets/" + name + ".png")
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: no sprite %q", name))
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: sprite %q: %v", name, err))
	}
	img := image.NewNRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	originals[name] = img
	return img
}

// spriteWidth is the width of sprite name drawn h pixels high.
func spriteWidth(name string, h int) int {
	spritesMu.Lock()
	defer spritesMu.Unlock()
	b := original(name).Bounds()
	return max(1, (b.Dx()*h+b.Dy()/2)/b.Dy())
}

// spriteAt is sprite name scaled to h pixels high (bilinear, on premultiplied
// alpha), cached.
func spriteAt(name string, h int) *image.NRGBA {
	w := spriteWidth(name, h)
	spritesMu.Lock()
	defer spritesMu.Unlock()
	key := fmt.Sprintf("%s@%d", name, h)
	if img, ok := scaled[key]; ok {
		return img
	}
	src := original(name)
	img := resizeNRGBA(src, w, h)
	scaled[key] = img
	return img
}

func resizeNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Shrinking a lot with bilinear sampling aliases; average over the source
	// pixels each destination pixel covers instead (box filter), and sample
	// bilinearly when enlarging.
	at := func(x, y int) [4]float64 {
		x, y = min(max(x, 0), sw-1), min(max(y, 0), sh-1)
		p := src.Pix[y*src.Stride+4*x:]
		a := float64(p[3]) / 255
		return [4]float64{float64(p[0]) * a, float64(p[1]) * a, float64(p[2]) * a, float64(p[3])}
	}
	sx, sy := float64(sw)/float64(w), float64(sh)/float64(h)
	for y := range h {
		for x := range w {
			var acc [4]float64
			if sx > 1 || sy > 1 {
				x0, x1 := int(float64(x)*sx), max(int(float64(x)*sx)+1, int(float64(x+1)*sx))
				y0, y1 := int(float64(y)*sy), max(int(float64(y)*sy)+1, int(float64(y+1)*sy))
				n := 0.0
				for yy := y0; yy < y1; yy++ {
					for xx := x0; xx < x1; xx++ {
						p := at(xx, yy)
						for c := range 4 {
							acc[c] += p[c]
						}
						n++
					}
				}
				for c := range 4 {
					acc[c] /= n
				}
			} else {
				fx, fy := (float64(x)+0.5)*sx-0.5, (float64(y)+0.5)*sy-0.5
				ix, iy := int(fx), int(fy)
				if fx < 0 {
					ix = -1
				}
				if fy < 0 {
					iy = -1
				}
				tx, ty := fx-float64(ix), fy-float64(iy)
				p00, p10, p01, p11 := at(ix, iy), at(ix+1, iy), at(ix, iy+1), at(ix+1, iy+1)
				for c := range 4 {
					acc[c] = (p00[c]*(1-tx)+p10[c]*tx)*(1-ty) + (p01[c]*(1-tx)+p11[c]*tx)*ty
				}
			}
			o := dst.Pix[y*dst.Stride+4*x:]
			alpha := acc[3]
			o[3] = uint8(alpha + 0.5)
			if alpha > 0 {
				for c := range 3 {
					o[c] = uint8(min(255, acc[c]*255/alpha+0.5))
				}
			}
		}
	}
	return dst
}

// drawSprite blends sprite onto img with its top-left corner at (x, y).
func drawSprite(img *image.RGBA, sprite *image.NRGBA, x, y int) {
	b := sprite.Bounds()
	for sy := range b.Dy() {
		for sx := range b.Dx() {
			dx, dy := x+sx, y+sy
			if dx < 0 || dy < 0 || dx >= img.Rect.Dx() || dy >= img.Rect.Dy() {
				continue
			}
			s := sprite.Pix[sy*sprite.Stride+4*sx:]
			a := uint32(s[3])
			if a == 0 {
				continue
			}
			d := img.Pix[dy*img.Stride+4*dx:]
			for c := range 3 {
				d[c] = uint8((uint32(s[c])*a + uint32(d[c])*(255-a) + 127) / 255)
			}
		}
	}
}
