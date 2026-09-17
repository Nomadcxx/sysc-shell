package render

import (
	"fmt"
	"image"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type weatherKind uint8

const (
	weatherClear weatherKind = iota
	weatherPartlyCloudy
	weatherCloudy
	weatherFog
	weatherRain
	weatherSnow
	weatherHeavySnow
	weatherThunderstorm
)

// ponytail: fixed particle counts cap CPU work; if a benchmark shows the
// visual grain is too coarse, raise them only with measured tile/binning work.
const (
	weatherRainParticles          = 64
	weatherSnowParticles          = 48
	weatherHeavySnowParticles     = 72
	weatherMaxRainLength          = 18
	weatherMaxSnowRadius          = 3
	weatherLightningSegments      = 7
	weatherMaxLightningSegmentPts = 24
)

func weatherKindFor(variant ui.EffectVariant) (weatherKind, error) {
	switch variant {
	case ui.WeatherClear:
		return weatherClear, nil
	case ui.WeatherPartlyCloudy:
		return weatherPartlyCloudy, nil
	case ui.WeatherCloudy:
		return weatherCloudy, nil
	case ui.WeatherFog:
		return weatherFog, nil
	case ui.WeatherRain:
		return weatherRain, nil
	case ui.WeatherSnow:
		return weatherSnow, nil
	case ui.WeatherHeavySnow:
		return weatherHeavySnow, nil
	case ui.WeatherThunderstorm:
		return weatherThunderstorm, nil
	default:
		return 0, fmt.Errorf("render: unsupported weather variant %d", variant)
	}
}

func paintWeatherEffect(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase float64) error {
	kind, err := weatherKindFor(spec.Variant)
	if err != nil {
		return err
	}
	intensity := effectIntensity(spec.Intensity)
	switch kind {
	case weatherClear:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .10)
		paintCelestial(c, box, mask, style, spec, phase, intensity)
	case weatherPartlyCloudy:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .28)
		paintCelestial(c, box, mask, style, spec, phase, intensity)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, .78)
	case weatherCloudy:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .56)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, 1)
	case weatherFog:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .42)
		paintFogHaze(c, box, mask, style, spec, phase, intensity)
	case weatherRain:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .25)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, .88)
		paintRain(c, box, mask, style, spec, phase, intensity)
	case weatherSnow:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .20)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, .82)
		paintSnow(c, box, mask, style, spec, phase, intensity, weatherSnowParticles, 1)
	case weatherHeavySnow:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .34)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, 1)
		paintSnow(c, box, mask, style, spec, phase, intensity, weatherHeavySnowParticles, 1.25)
	case weatherThunderstorm:
		paintSkyCloudWash(c, box, mask, style, spec, phase, intensity, .50)
		paintCloudScene(c, box, mask, style, spec, phase, intensity, 1)
		paintRain(c, box, mask, style, spec, phase, intensity)
		paintLightning(c, box, mask, style, spec, phase, intensity)
	}
	return nil
}

// paintCelestial gives clear states a single readable focal form. The rays and
// halo move more slowly than the surface phase, which keeps the hero calm while
// still making a paused frame visibly different from its neighbours.
func paintCelestial(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	celestial := weatherRole(style.Accent, style.Foreground)
	if celestial.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	width, height := float64(box.W), float64(box.H)
	short := float64(min(box.W, box.H))
	breath := .96 + .04*math.Sin(2*math.Pi*(time*.22+weatherUnit(spec.Seed, 41, 1)))
	cx := width*.69 + math.Sin(2*math.Pi*(time*.18+weatherUnit(spec.Seed, 41, 2)))*width*.035
	cy := height*.29 + math.Sin(2*math.Pi*(time*.13+weatherUnit(spec.Seed, 41, 3)))*height*.025
	radius := short * .18 * breath
	if radius < 3 {
		radius = 3
	}

	halo := LerpColor(celestial, style.Foreground, .36)
	drawWeatherEllipse(c, box, mask, cx, cy, radius*1.72, radius*1.72,
		halo, weatherAlpha(halo.A, intensity*.18))
	drawWeatherEllipse(c, box, mask, cx, cy, radius*1.30, radius*1.30,
		halo, weatherAlpha(halo.A, intensity*(.18+.05*breath)))

	ray := LerpColor(celestial, style.Foreground, .22)
	rayCount := 8
	rotation := 2 * math.Pi * (time*.11 + weatherUnit(spec.Seed, 42, 1))
	for i := 0; i < rayCount; i++ {
		angle := rotation + float64(i)*2*math.Pi/float64(rayCount)
		inner := radius * 1.28
		outer := radius * (1.62 + .08*math.Sin(2*math.Pi*(time*.17+float64(i))))
		drawWeatherStroke(c, box, mask,
			cx+math.Cos(angle)*inner, cy+math.Sin(angle)*inner,
			cx+math.Cos(angle)*outer, cy+math.Sin(angle)*outer,
			max(1, short*.018), ray, weatherAlpha(ray.A, intensity*.40))
	}

	drawWeatherEllipse(c, box, mask, cx, cy, radius, radius,
		celestial, weatherAlpha(celestial.A, intensity*.92))
	highlight := LerpColor(celestial, style.Foreground, .55)
	drawWeatherEllipse(c, box, mask, cx-radius*.22, cy-radius*.24, radius*.40, radius*.32,
		highlight, weatherAlpha(highlight.A, intensity*.42))
}

// paintCloudScene uses two overlapping masses so cloud states read as a form,
// not as another full-card wash. Their bounded drift supplies the slow lift
// suggested by the Meteocons reference without moving the content layout.
func paintCloudScene(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 || density <= 0 {
		return
	}
	base := weatherRole(style.ContainerHighest, style.Foreground)
	if base.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	width, height := float64(box.W), float64(box.H)
	driftX := math.Sin(2*math.Pi*(time*.34+weatherUnit(spec.Seed, 51, 1))) * width * .045
	driftY := math.Sin(2*math.Pi*(time*.24+weatherUnit(spec.Seed, 51, 2))) * height * .035

	shadow := LerpColor(base, style.Background, .30)
	paintCloudMass(c, box, mask, width*.38+driftX*.65, height*.57+driftY*.65,
		width*.66, height*.31, shadow, weatherAlpha(shadow.A, intensity*density*.44))

	cloud := LerpColor(base, style.Foreground, .43)
	paintCloudMass(c, box, mask, width*.60+driftX, height*.73+driftY,
		width*.92, height*.39, cloud, weatherAlpha(cloud.A, intensity*density*.78))
}

type weatherCloudPuff struct {
	x, y, rx, ry float64
}

func paintCloudMass(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy, width, height float64, col Color, alpha uint8) {
	if alpha == 0 || width <= 0 || height <= 0 {
		return
	}
	// The lower ellipse gives the mass a stable base; the puffs provide the
	// overlapping silhouette that distinguishes weather from a gradient.
	drawWeatherEllipse(c, box, mask, cx, cy+height*.10, width*.50, height*.27, col, alpha)
	for _, puff := range []weatherCloudPuff{
		{x: -.34, y: -.02, rx: .25, ry: .42},
		{x: -.12, y: -.15, rx: .28, ry: .55},
		{x: .14, y: -.12, rx: .30, ry: .49},
		{x: .36, y: .02, rx: .24, ry: .36},
	} {
		drawWeatherEllipse(c, box, mask, cx+width*puff.x, cy+height*puff.y,
			width*puff.rx, height*puff.ry, col, alpha)
	}
}

func drawWeatherEllipse(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy, rx, ry float64, col Color, alpha uint8) {
	if alpha == 0 || rx <= 0 || ry <= 0 || math.IsNaN(cx) || math.IsNaN(cy) {
		return
	}
	minX := max(int(math.Floor(cx-rx-1)), 0)
	maxX := min(int(math.Ceil(cx+rx+1)), box.W)
	minY := max(int(math.Floor(cy-ry-1)), 0)
	maxY := min(int(math.Ceil(cy+ry+1)), box.H)
	shortRadius := min(rx, ry)
	if shortRadius <= 0 {
		return
	}
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			dx := (float64(x) + .5 - cx) / rx
			dy := (float64(y) + .5 - cy) / ry
			edge := (1 - math.Hypot(dx, dy)) * shortRadius
			coverage := clampEffect(.5+edge, 0, 1)
			if coverage > 0 {
				localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
			}
		}
	}
}

func drawWeatherStroke(c *Canvas, box ui.Rect, mask *image.Alpha, x0, y0, x1, y1, width float64, col Color, alpha uint8) {
	if alpha == 0 || width <= 0 {
		return
	}
	steps := max(int(math.Ceil(math.Hypot(x1-x0, y1-y0))), 1)
	steps = min(steps, 96)
	radius := width / 2
	for step := 0; step <= steps; step++ {
		t := float64(step) / float64(steps)
		x := x0 + (x1-x0)*t
		y := y0 + (y1-y0)*t
		drawWeatherEllipse(c, box, mask, x, y, radius, radius, col, alpha)
	}
}

func paintSkyCloudWash(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	sky := weatherRole(style.Background, style.Accent)
	cloud := weatherRole(style.ContainerHighest, style.Secondary)
	if sky.A == 0 || cloud.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	density = clampEffect(density, 0, 1)
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		for x := x0; x < x1; x++ {
			u := (float64(x-box.X) + .5) / float64(box.W)
			wave := .5 + .5*math.Sin(2*math.Pi*(u*1.15+v*.70+time*.08))
			wave += .25 * math.Sin(2*math.Pi*(u*.45-v*1.20-time*.045))
			wave = clampEffect(wave, 0, 1)
			mix := clampEffect(density*.58+wave*.42, 0, 1)
			col := LerpColor(sky, cloud, mix)
			col.A = weatherAlpha(col.A, intensity*(.025+mix*.12))
			blendWeatherPixel(c, box, mask, x, y, col, 255)
		}
	}
}

func paintFogHaze(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	base := weatherRole(style.Background, style.Container)
	haze := weatherRole(style.Foreground, style.Track)
	if base.A == 0 || haze.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		for x := x0; x < x1; x++ {
			u := (float64(x-box.X) + .5) / float64(box.W)
			field := .5 + .5*math.Sin(2*math.Pi*(u*1.35+v*2.10+time*.06))
			field += .20 * math.Sin(2*math.Pi*(u*.55-v*.80-time*.035))
			field = clampEffect(field, 0, 1)
			col := LerpColor(base, haze, .25+.35*field)
			col.A = weatherAlpha(col.A, intensity*(.045+.13*field))
			blendWeatherPixel(c, box, mask, x, y, col, 255)
		}
	}
}

func paintRain(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	ink := weatherRole(style.Accent, style.Foreground)
	if ink.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	width, height := float64(box.W), float64(box.H)
	cycleHeight := height + weatherMaxRainLength + 1
	for i := uint64(0); i < weatherRainParticles; i++ {
		xSeed := weatherUnit(spec.Seed, i, 1)
		ySeed := weatherUnit(spec.Seed, i, 2)
		fallSpeed := .72 + weatherUnit(spec.Seed, i, 3)*.46
		localX := positiveMod(xSeed*width+time*width*.10, width)
		localY := positiveMod(ySeed*cycleHeight+time*cycleHeight*fallSpeed, cycleHeight) - weatherMaxRainLength
		length := 7 + int(weatherUnit(spec.Seed, i, 4)*float64(weatherMaxRainLength-6))
		slant := 2 + int(weatherUnit(spec.Seed, i, 5)*4)
		for step := 0; step < length; step++ {
			fraction := float64(step) / float64(max(length-1, 1))
			px := int(math.Floor(positiveMod(localX+fraction*float64(slant), width)))
			py := int(math.Floor(localY + float64(step)))
			coverage := .62 + .30*math.Min(fraction, 1-fraction)
			localWeatherPixel(c, box, mask, px, py, ink, weatherAlpha(ink.A, intensity*(.35+.35*weatherUnit(spec.Seed, i, 6))), coverage)
		}
	}
}

func paintSnow(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64, count int, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 || count <= 0 {
		return
	}
	ink := weatherRole(style.Foreground, style.Tertiary)
	if ink.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	width, height := float64(box.W), float64(box.H)
	cycleHeight := height + 2*weatherMaxSnowRadius + 1
	for i := uint64(0); i < uint64(count); i++ {
		xSeed := weatherUnit(spec.Seed, i, 11)
		ySeed := weatherUnit(spec.Seed, i, 12)
		radius := 1 + int(weatherUnit(spec.Seed, i, 13)*float64(weatherMaxSnowRadius))
		fallSpeed := .20 + weatherUnit(spec.Seed, i, 14)*.24
		localY := positiveMod(ySeed*cycleHeight+time*cycleHeight*fallSpeed, cycleHeight) - weatherMaxSnowRadius
		drift := math.Sin(2*math.Pi*(time*.20+xSeed)) * (1 + weatherUnit(spec.Seed, i, 15)*2)
		localX := positiveMod(xSeed*width+drift, width)
		flakeAlpha := intensity * density * (.35 + .35*weatherUnit(spec.Seed, i, 16))
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				distance := math.Hypot(float64(dx), float64(dy))
				coverage := clampEffect(float64(radius)+.65-distance, 0, 1)
				if coverage == 0 {
					continue
				}
				px := int(math.Round(localX)) + dx
				py := int(math.Round(localY)) + dy
				localWeatherPixel(c, box, mask, px, py, ink, weatherAlpha(ink.A, flakeAlpha), coverage)
			}
		}
	}
}

func paintLightning(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	pulse := lightningPulse(phase, spec.Speed, spec.Seed)
	if pulse <= 0 {
		return
	}
	ink := weatherRole(style.Tertiary, style.Foreground)
	if ink.A == 0 {
		return
	}
	// The glow is intentionally soft and bounded to the same rounded mask as
	// the bolt, so a flash cannot illuminate outside its weather card.
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		for x := x0; x < x1; x++ {
			u := (float64(x-box.X) + .5) / float64(box.W)
			variation := .5 + .5*math.Sin(2*math.Pi*(u*.55+v*.90))
			col := ink
			col.A = weatherAlpha(ink.A, intensity*pulse*(.025+.04*variation))
			blendWeatherPixel(c, box, mask, x, y, col, 255)
		}
	}

	if pulse < .18 {
		return
	}
	previousX := int(weatherUnit(spec.Seed, 91, 1) * float64(max(box.W, 1)))
	previousY := 0
	for segment := 1; segment <= weatherLightningSegments; segment++ {
		nextY := segment * box.H / weatherLightningSegments
		offset := int((weatherUnit(spec.Seed, uint64(91+segment), 2)*2 - 1) * float64(max(box.W/8, 2)))
		nextX := clampInt(previousX+offset, 0, max(box.W-1, 0))
		drawWeatherSegment(c, box, mask, previousX, previousY, nextX, nextY, ink,
			weatherAlpha(ink.A, intensity*pulse*.80), .70)
		previousX, previousY = nextX, nextY
	}
}

func drawWeatherSegment(c *Canvas, box ui.Rect, mask *image.Alpha, x0, y0, x1, y1 int, col Color, alpha uint8, coverage float64) {
	steps := max(absEffect(x1-x0), absEffect(y1-y0))
	steps = clampInt(steps, 1, weatherMaxLightningSegmentPts)
	for step := 0; step <= steps; step++ {
		t := float64(step) / float64(steps)
		x := int(math.Round(float64(x0) + float64(x1-x0)*t))
		y := int(math.Round(float64(y0) + float64(y1-y0)*t))
		localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
	}
}

func localWeatherPixel(c *Canvas, box ui.Rect, mask *image.Alpha, x, y int, col Color, alpha uint8, coverage float64) {
	if box.W <= 0 || box.H <= 0 || x < 0 || y < 0 || x >= box.W || y >= box.H {
		return
	}
	// Clamp the local coordinate before forming the absolute coordinate. The
	// range check above keeps off-card particles from being folded onto an edge.
	x = clampInt(x, 0, box.W-1)
	y = clampInt(y, 0, box.H-1)
	absoluteX, xOK := checkedEffectAdd(box.X, x)
	absoluteY, yOK := checkedEffectAdd(box.Y, y)
	if !xOK || !yOK {
		return
	}
	blendWeatherPixel(c, box, mask, absoluteX, absoluteY,
		Color{R: col.R, G: col.G, B: col.B, A: alpha}, weatherCoverage(coverage))
}

func blendWeatherPixel(c *Canvas, box ui.Rect, mask *image.Alpha, x, y int, col Color, coverage uint8) {
	if c == nil || mask == nil || col.A == 0 || coverage == 0 ||
		box.W <= 0 || box.H <= 0 || box.W > maxEffectDimension || box.H > maxEffectDimension ||
		box.W > maxEffectPixels/box.H {
		return
	}
	boxRight, ok := checkedEffectAdd(box.X, box.W)
	if !ok {
		return
	}
	boxBottom, ok := checkedEffectAdd(box.Y, box.H)
	if !ok {
		return
	}
	if x < box.X || y < box.Y || x >= boxRight || y >= boxBottom ||
		x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return
	}
	if c.restrict.W > 0 && c.restrict.H > 0 {
		restrictRight, rightOK := checkedEffectAdd(c.restrict.X, c.restrict.W)
		restrictBottom, bottomOK := checkedEffectAdd(c.restrict.Y, c.restrict.H)
		if !rightOK || !bottomOK || x < c.restrict.X || y < c.restrict.Y || x >= restrictRight || y >= restrictBottom {
			return
		}
	}
	b := mask.Bounds()
	mx, xOK := checkedEffectAdd(b.Min.X, x-box.X)
	my, yOK := checkedEffectAdd(b.Min.Y, y-box.Y)
	if !xOK || !yOK || !image.Pt(mx, my).In(b) {
		return
	}
	maskCoverage := uint32(mask.AlphaAt(mx, my).A)
	combined := uint32(coverage) * maskCoverage / 255
	alpha := uint32(col.A) * combined / 255
	if alpha == 0 {
		return
	}
	src := col.premultiply()
	for i := range src {
		src[i] = byte(uint32(src[i]) * combined / 255)
	}
	if c.Stride <= 0 || len(c.Pix) < 4 || y > (len(c.Pix)-4)/c.Stride {
		return
	}
	rowOffset := y * c.Stride
	if x > (len(c.Pix)-rowOffset-4)/4 {
		return
	}
	offset := rowOffset + x*4
	blendPixel(c.Pix[offset:offset+4], src, alpha)
}

func weatherRole(primary, fallback Color) Color {
	if primary.A != 0 {
		return primary
	}
	return fallback
}

func weatherAlpha(base uint8, amount float64) uint8 {
	amount = clampEffect(amount, 0, 1)
	return uint8(math.Round(float64(base) * amount))
}

func weatherCoverage(coverage float64) uint8 {
	return uint8(math.Round(clampEffect(coverage, 0, 1) * 255))
}

func effectIntensity(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return clampEffect(value, 0, 1)
}

func effectTime(phase, speed float64) float64 {
	if math.IsNaN(phase) || math.IsInf(phase, 0) {
		phase = 0
	}
	if math.IsNaN(speed) || math.IsInf(speed, 0) {
		speed = 0
	}
	phase = positiveMod(phase, 1)
	return phase * clampEffect(speed, 0, 4)
}

func lightningPulse(phase, speed float64, seed uint64) float64 {
	time := effectTime(phase, speed) + weatherUnit(seed, 90, 7)
	cycle := positiveMod(time*2.0, 1)
	return math.Max(pulseAt(cycle, .16, .045), .72*pulseAt(cycle, .29, .030))
}

func pulseAt(value, centre, width float64) float64 {
	distance := math.Abs(value - centre)
	if distance >= width {
		return 0
	}
	return 1 - distance/width
}

func clampEffect(value, lo, hi float64) float64 {
	if math.IsNaN(value) {
		return lo
	}
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func positiveMod(value, modulus float64) float64 {
	if modulus <= 0 || math.IsNaN(value) || math.IsNaN(modulus) || math.IsInf(value, 0) || math.IsInf(modulus, 0) {
		return 0
	}
	value = math.Mod(value, modulus)
	if value < 0 {
		value += modulus
	}
	return value
}

func absEffect(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func weatherUnit(seed, index, salt uint64) float64 {
	return float64(weatherMix(seed+index*0x9e3779b97f4a7c15+salt*0xd1b54a32d192ed03)>>11) * (1.0 / 9007199254740992.0)
}

func weatherMix(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}
