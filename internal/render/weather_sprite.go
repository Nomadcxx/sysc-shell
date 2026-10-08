package render

import (
	"image"
	"math"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// weatherSprite is one baked, premultiplied BGRA form. The weather program
// never evaluates noise per frame: every textured form is baked once at the
// physical size it is drawn, and a frame is a set of unscaled blits.
type weatherSprite struct {
	w, h int
	pix  []byte
}

// weatherSpriteMaxEdge bounds one sprite, as maxEffectDimension bounds the box.
const weatherSpriteMaxEdge = maxEffectDimension

func weatherValueNoise(x, y float64, seed uint64) float64 {
	xi, yi := math.Floor(x), math.Floor(y)
	fx, fy := x-xi, y-yi
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	h := func(dx, dy float64) float64 {
		k := uint64(int64(xi+dx))*0x9e3779b97f4a7c15 ^ uint64(int64(yi+dy))*0xc2b2ae3d27d4eb4f
		return weatherUnit(seed, k, 3)
	}
	a, b, c, d := h(0, 0), h(1, 0), h(0, 1), h(1, 1)
	return a + (b-a)*fx + (c-a)*fy + (a-b-c+d)*fx*fy
}

// weatherFBM is fractal value noise in [0, 1).
func weatherFBM(x, y float64, seed uint64, octaves int) float64 {
	v, amp, f, norm := 0.0, .5, 1.0, 0.0
	for i := 0; i < octaves; i++ {
		v += amp * weatherValueNoise(x*f, y*f, seed+uint64(i)*17)
		norm += amp
		f *= 2.03
		amp *= .5
	}
	if norm == 0 {
		return 0
	}
	return math.Min(v/norm, math.Nextafter(1, 0))
}

// bakeWeatherSprite evaluates shade on a half-resolution grid and upsamples it
// bilinearly into a w x h sprite. Noise fields are smooth, so the half grid
// costs a quarter of the evaluations and shows no difference.
func bakeWeatherSprite(w, h int, shade func(u, v float64) Color) *weatherSprite {
	w, h = clampInt(w, 0, weatherSpriteMaxEdge), clampInt(h, 0, weatherSpriteMaxEdge)
	s := &weatherSprite{w: w, h: h, pix: make([]byte, w*h*4)}
	if w == 0 || h == 0 {
		return s
	}
	gw, gh := max((w+1)/2, 1), max((h+1)/2, 1)
	grid := make([]Color, (gw+1)*(gh+1))
	for gy := 0; gy <= gh; gy++ {
		for gx := 0; gx <= gw; gx++ {
			grid[gy*(gw+1)+gx] = shade(float64(gx)/float64(gw), float64(gy)/float64(gh))
		}
	}
	for y := 0; y < h; y++ {
		fy := float64(y) / float64(max(h-1, 1)) * float64(gh)
		y0 := min(int(fy), gh-1)
		ty := fy - float64(y0)
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(max(w-1, 1)) * float64(gw)
			x0 := min(int(fx), gw-1)
			tx := fx - float64(x0)
			top := LerpColor(grid[y0*(gw+1)+x0], grid[y0*(gw+1)+x0+1], tx)
			bottom := LerpColor(grid[(y0+1)*(gw+1)+x0], grid[(y0+1)*(gw+1)+x0+1], tx)
			p := LerpColor(top, bottom, ty).premultiply()
			copy(s.pix[(y*w+x)*4:], p[:])
		}
	}
	return s
}

type weatherSpriteKey struct {
	kind string
	seed uint64
	a, b Color
	w, h int
}

const weatherSpriteCap = 32

// The cache is process-wide: every weather surface shares it. Paints run on
// the Wayland owner, but tests paint in parallel, so it is locked.
var weatherSprites = struct {
	sync.Mutex
	m     map[weatherSpriteKey]*weatherSprite
	order []weatherSpriteKey // least recently used first
}{m: map[weatherSpriteKey]*weatherSprite{}}

func weatherSpriteFor(key weatherSpriteKey, bake func() *weatherSprite) *weatherSprite {
	weatherSprites.Lock()
	defer weatherSprites.Unlock()
	if s, ok := weatherSprites.m[key]; ok {
		weatherSpriteTouch(key)
		return s
	}
	s := bake()
	weatherSprites.m[key] = s
	weatherSprites.order = append(weatherSprites.order, key)
	for len(weatherSprites.order) > weatherSpriteCap {
		delete(weatherSprites.m, weatherSprites.order[0])
		weatherSprites.order = weatherSprites.order[1:]
	}
	return s
}

// weatherSpriteTouch moves key to the most recently used end in place. It runs
// for every blit of every frame, so it must not allocate.
func weatherSpriteTouch(key weatherSpriteKey) {
	order := weatherSprites.order
	for i, k := range order {
		if k == key {
			copy(order[i:], order[i+1:])
			order[len(order)-1] = key
			return
		}
	}
}

func weatherSpriteLen() int {
	weatherSprites.Lock()
	defer weatherSprites.Unlock()
	return len(weatherSprites.m)
}

func weatherSpriteReset() {
	weatherSprites.Lock()
	defer weatherSprites.Unlock()
	weatherSprites.m = map[weatherSpriteKey]*weatherSprite{}
	weatherSprites.order = nil
}

// blitWeatherSprite composites s at box-local (x, y) with a global alpha,
// clipped to the box, the canvas, the canvas restriction and the shape mask.
func blitWeatherSprite(c *Canvas, box ui.Rect, mask *image.Alpha, s *weatherSprite, x, y int, alpha float64) {
	a := uint32(math.Round(clampEffect(alpha, 0, 1) * 255))
	if c == nil || mask == nil || s == nil || a == 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	cx0, cy0, cx1, cy1 := c.clip(box)
	b := mask.Bounds()
	// The sprite's visible span, in sprite coordinates: the intersection of
	// the canvas clip and the mask, both shifted by the sprite's origin.
	ox, oy := box.X+x, box.Y+y
	sx0, sx1 := max(0, cx0-ox), min(s.w, cx1-ox, box.X+b.Dx()-ox)
	sy0, sy1 := max(0, cy0-oy), min(s.h, cy1-oy, box.Y+b.Dy()-oy)
	for sy := sy0; sy < sy1; sy++ {
		py := oy + sy
		maskRow := (py - box.Y) * mask.Stride
		row := py * c.Stride
		for sx := sx0; sx < sx1; sx++ {
			i := (sy*s.w + sx) * 4
			if s.pix[i+3] == 0 {
				continue
			}
			px := ox + sx
			k := a * uint32(mask.Pix[maskRow+px-box.X]) / 255
			if k == 0 {
				continue
			}
			src := [4]byte{
				byte(uint32(s.pix[i]) * k / 255), byte(uint32(s.pix[i+1]) * k / 255),
				byte(uint32(s.pix[i+2]) * k / 255), byte(uint32(s.pix[i+3]) * k / 255),
			}
			if src[3] == 0 {
				continue
			}
			blendPixel(c.Pix[row+px*4:row+px*4+4], src, uint32(src[3]))
		}
	}
}
