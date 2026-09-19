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
	weatherRainParticles          = 150
	weatherSnowParticles          = 96
	weatherHeavySnowParticles     = 132
	weatherMaxRainLength          = 22
	weatherMaxSnowRadius          = 3
	weatherLightningSegments      = 7
	weatherMaxLightningSegmentPts = 24
	// weatherSceneAspect is the one shape every weather form is composed in.
	// It is the standalone hero's own aspect, so a wider consumer such as the
	// Control Centre shows more sky rather than a stretched sun or cloud.
	weatherSceneAspect = 1.20
	// weatherLightningOriginY is the cloud base, as a fraction of scene height.
	// It matches the front cloud mass's centre plus its half-height.
	weatherLightningOriginY = .52
)

// weatherSceneBox is the aspect-locked region the forms are drawn in, in the
// effect box's local coordinates. The sky still fills the whole effect box;
// only the forms are confined here, which is what stops a wide card from
// widening the mark. bias places the locked scene horizontally: zero centres
// it, minus one puts it against the leading edge.
func weatherSceneBox(box ui.Rect, bias float64) ui.Rect {
	if box.W <= 0 || box.H <= 0 {
		return ui.Rect{}
	}
	width := int(float64(box.H) * weatherSceneAspect)
	if width >= box.W {
		return ui.Rect{W: box.W, H: box.H}
	}
	spare := box.W - width
	// Floor rather than round, so a centred scene lands on the same pixel as
	// the (W-w)/2 the rest of the layout uses.
	offset := int(math.Floor(float64(spare) * (clampEffect(bias, -1, 1) + 1) * .5))
	return ui.Rect{X: clampInt(offset, 0, spare), W: width, H: box.H}
}

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
	// Every form is composed inside this one locked box; only the sky fills
	// the effect box, so a wide consumer shows more sky, never a wider mark.
	scene := weatherSceneBox(box, spec.SceneBias)
	switch kind {
	case weatherClear:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .10)
		paintCelestial(c, box, scene, mask, style, spec, phase, intensity)
	case weatherPartlyCloudy:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .28)
		paintCelestial(c, box, scene, mask, style, spec, phase, intensity)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, .78)
	case weatherCloudy:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .56)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, 1)
	case weatherFog:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .42)
		paintFogHaze(c, box, scene, mask, style, spec, phase, intensity)
	case weatherRain:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .25)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, .88)
		paintRain(c, box, scene, mask, style, spec, phase, intensity)
	case weatherSnow:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .20)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, .82)
		paintSnow(c, box, scene, mask, style, spec, phase, intensity, weatherSnowParticles, 1)
	case weatherHeavySnow:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .34)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, 1)
		paintSnow(c, box, scene, mask, style, spec, phase, intensity, weatherHeavySnowParticles, 1.25)
	case weatherThunderstorm:
		paintSkyCloudWash(c, box, mask, style, spec, intensity, .50)
		paintCloudScene(c, box, scene, mask, style, spec, phase, intensity, 1)
		paintRain(c, box, scene, mask, style, spec, phase, intensity)
		paintLightning(c, box, scene, mask, style, spec, phase, intensity)
	}
	return nil
}

// paintCelestial gives clear states a single readable focal form. The rays and
// halo move more slowly than the surface phase, which keeps the hero calm while
// still making a paused frame visibly different from its neighbours.
func paintCelestial(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	celestial := weatherRole(style.Accent, style.Foreground)
	if celestial.A == 0 {
		return
	}
	time := weatherLoopPhase(phase, spec.Speed)
	ox := float64(scene.X)
	width, height := float64(scene.W), float64(scene.H)
	short := float64(min(scene.W, scene.H))
	breath := .93 + .07*(.5+.5*math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 41, 1))))
	cx := ox + width*.50 + math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 41, 2)))*width*.055
	cy := height*.30 + math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 41, 3)))*height*.035
	radius := short * .18 * breath
	if radius < 3 {
		radius = 3
	}
	if spec.Night {
		paintMoon(c, box, mask, style, spec, time, cx, cy, radius, intensity)
		return
	}

	halo := LerpColor(celestial, style.Foreground, .36)
	haloBreath := .5 + .5*math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 43, 1)))
	drawWeatherEllipse(c, box, mask, cx, cy, radius*(1.62+.20*haloBreath), radius*(1.62+.20*haloBreath),
		halo, weatherAlpha(halo.A, intensity*(.20+.10*haloBreath)))
	drawWeatherEllipse(c, box, mask, cx, cy, radius*(1.22+.10*haloBreath), radius*(1.22+.10*haloBreath),
		halo, weatherAlpha(halo.A, intensity*(.24+.10*haloBreath)))

	ray := LerpColor(celestial, style.Foreground, .22)
	rayCount := 8
	rotation := 2 * math.Pi * (time + weatherUnit(spec.Seed, 42, 1))
	for i := 0; i < rayCount; i++ {
		angle := rotation + float64(i)*2*math.Pi/float64(rayCount)
		inner := radius * 1.28
		outer := radius * (1.62 + .10*math.Sin(2*math.Pi*(time+float64(i)*.17)))
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

func paintMoon(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, time, cx, cy, radius, intensity float64) {
	moon := LerpColor(style.Foreground, style.Secondary, .16)
	if moon.A == 0 {
		moon = style.Foreground
	}
	halo := LerpColor(style.Secondary, style.Foreground, .35)
	haloBreath := .5 + .5*math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 44, 1)))
	drawWeatherEllipse(c, box, mask, cx, cy, radius*(1.70+.18*haloBreath), radius*(1.70+.18*haloBreath),
		halo, weatherAlpha(halo.A, intensity*(.12+.06*haloBreath)))
	drawWeatherEllipse(c, box, mask, cx, cy, radius*(1.25+.08*haloBreath), radius*(1.25+.08*haloBreath),
		halo, weatherAlpha(halo.A, intensity*(.16+.06*haloBreath)))
	drawWeatherCrescent(c, box, mask, cx, cy, radius, cx+radius*.42, cy-radius*.08, radius*1.03,
		moon, weatherAlpha(moon.A, intensity*.88))
}

func drawWeatherCrescent(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy, radius, cutX, cutY, cutRadius float64, col Color, alpha uint8) {
	if alpha == 0 || radius <= 0 || cutRadius <= 0 {
		return
	}
	minX := max(int(math.Floor(cx-radius-1)), 0)
	maxX := min(int(math.Ceil(cx+radius+1)), box.W)
	minY := max(int(math.Floor(cy-radius-1)), 0)
	maxY := min(int(math.Ceil(cy+radius+1)), box.H)
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			coverage := weatherEllipseCoverage(x, y, cx, cy, radius, radius)
			coverage -= weatherEllipseCoverage(x, y, cutX, cutY, cutRadius, cutRadius)
			if coverage > 0 {
				localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
			}
		}
	}
}

// paintCloudScene uses parallax masses so cloud states read as a form, not as
// another full-card wash. The closed phase gives the front and rear layers
// different lift and drift, like the staged Pixel icon motion.
func paintCloudScene(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 || density <= 0 {
		return
	}
	base := weatherRole(style.ContainerHighest, style.Capsule)
	if base.A == 0 {
		return
	}
	time := weatherLoopPhase(phase, spec.Speed)
	height := float64(scene.H)
	sceneWidth := float64(scene.W)
	sceneX := float64(scene.X)
	backX := math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 51, 1))) * sceneWidth * .075
	backY := math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 51, 2))) * height * .045
	frontX := math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 52, 1))) * sceneWidth * .11
	frontY := math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 52, 2))) * height * .075
	breath := .5 + .5*math.Sin(2*math.Pi*(time+weatherUnit(spec.Seed, 53, 1)))

	// One bank with depth, not four stacked blobs. The masses differ in scale
	// and position far more than in colour: wide colour steps between similar
	// shapes are what made the overlaps read as lighter lenses rather than as
	// one silhouette. The bank is centred so it works as the hero's mark.
	back := LerpColor(base, style.Foreground, .34)
	paintCloudMass(c, box, mask, sceneX+sceneWidth*.34+backX, height*(.40+.02*breath)+backY,
		sceneWidth*.50, height*.21, back, weatherAlpha(back.A, intensity*density*(.58+.08*breath)))

	shadow := LerpColor(base, style.Background, .32)
	paintCloudMass(c, box, mask, sceneX+sceneWidth*.52+frontX*.72, height*(.61+.02*breath)+frontY*.72,
		sceneWidth*.78, height*.26, shadow, weatherAlpha(shadow.A, intensity*density*(.72+.08*breath)))

	cloud := LerpColor(base, style.Foreground, .52)
	paintCloudMass(c, box, mask, sceneX+sceneWidth*.48+frontX, height*(.52+.02*breath)+frontY,
		sceneWidth*.74, height*.31, cloud, weatherAlpha(cloud.A, intensity*density*(.90+.08*breath)))

	// A small lit cap on the front mass's shoulder, not a second cloud.
	highlight := LerpColor(cloud, style.Foreground, .26)
	paintCloudMass(c, box, mask, sceneX+sceneWidth*.40+frontX*.60, height*(.45+.015*breath)+frontY*.55,
		sceneWidth*.38, height*.17, highlight, weatherAlpha(highlight.A, intensity*density*(.62+.06*breath)))
}

type weatherCloudPuff struct {
	x, y, rx, ry float64
}

func paintCloudMass(c *Canvas, box ui.Rect, mask *image.Alpha, cx, cy, width, height float64, col Color, alpha uint8) {
	if alpha == 0 || width <= 0 || height <= 0 {
		return
	}
	// The lower ellipse gives the mass a stable base; the puffs provide the
	// overlapping silhouette that distinguishes weather from a gradient. Keep
	// the union as one coverage field so intersections do not stack alpha and
	// turn into visible bubbles.
	ellipses := [...]weatherCloudPuff{
		{x: 0, y: .10, rx: .50, ry: .27},
		{x: -.34, y: -.02, rx: .25, ry: .42},
		{x: -.12, y: -.15, rx: .28, ry: .55},
		{x: .14, y: -.12, rx: .30, ry: .49},
		{x: .36, y: .02, rx: .24, ry: .36},
	}
	minX, maxX := box.W, 0
	minY, maxY := box.H, 0
	for _, ellipse := range ellipses {
		minX = min(minX, int(math.Floor(cx+width*(ellipse.x-ellipse.rx)-1)))
		maxX = max(maxX, int(math.Ceil(cx+width*(ellipse.x+ellipse.rx)+1)))
		minY = min(minY, int(math.Floor(cy+height*(ellipse.y-ellipse.ry)-1)))
		maxY = max(maxY, int(math.Ceil(cy+height*(ellipse.y+ellipse.ry)+1)))
	}
	minX, maxX = max(minX, 0), min(maxX, box.W)
	minY, maxY = max(minY, 0), min(maxY, box.H)
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			coverage := 0.0
			for _, ellipse := range ellipses {
				coverage = max(coverage, weatherEllipseCoverage(x, y,
					cx+width*ellipse.x, cy+height*ellipse.y,
					width*ellipse.rx, height*ellipse.ry))
			}
			if coverage > 0 {
				localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
			}
		}
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
			coverage := weatherEllipseCoverage(x, y, cx, cy, rx, ry)
			if coverage > 0 {
				localWeatherPixel(c, box, mask, x, y, col, alpha, coverage)
			}
		}
	}
}

func weatherEllipseCoverage(x, y int, cx, cy, rx, ry float64) float64 {
	if rx <= 0 || ry <= 0 {
		return 0
	}
	shortRadius := min(rx, ry)
	dx := (float64(x) + .5 - cx) / rx
	dy := (float64(y) + .5 - cy) / ry
	// Squared distance first. Coverage only leaves 0 or 1 within half a pixel
	// of the rim, so the interior and the exterior of every puff answer without
	// a square root at all -- and math.Hypot's overflow handling buys nothing
	// on values already normalised by the radii. This is the cloud inner loop.
	square := dx*dx + dy*dy
	if inner := 1 - .5/shortRadius; inner > 0 && square <= inner*inner {
		return 1
	}
	if outer := 1 + .5/shortRadius; square >= outer*outer {
		return 0
	}
	edge := (1 - math.Sqrt(square)) * shortRadius
	return clampEffect(.5+edge, 0, 1)
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

func paintSkyCloudWash(c *Canvas, box ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, intensity, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	skyTop, skyBottom, cloud := weatherSkyColors(style, spec.Night)
	if skyTop.A == 0 || skyBottom.A == 0 || cloud.A == 0 {
		return
	}
	density = clampEffect(density, 0, 1)
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	// The sky is deliberately static. Its shape still varies per seed and per
	// state, but nothing here reads the phase: a sky that travels is the
	// animated gradient this scene grammar exists to replace, and it competes
	// with the forms that are supposed to carry the motion.
	// Direction B's sky is a vertical gradient: static, and varying only down
	// the card. That is one colour per row rather than one per pixel, which is
	// what makes a full-card sky affordable at the Control Centre's size -- the
	// effect loop never settles, so this runs on every frame the panel is open.
	skew := weatherUnit(spec.Seed, 61, 1)
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		depth := clampEffect(v+.035*math.Sin(2*math.Pi*(v*.30+skew)), 0, 1)
		col := LerpColor(skyTop, skyBottom, depth)
		veil := density * (.10 + .10*(.5+.5*math.Sin(2*math.Pi*(v*.55+skew))))
		col = LerpColor(col, cloud, clampEffect(veil, 0, 1))
		col.A = weatherAlpha(col.A, intensity*(.58+.16*(.5+.5*math.Sin(2*math.Pi*(v*.70+skew)))))
		blendWeatherRow(c, box, mask, y, x0, x1, col)
	}
}

func weatherSkyColors(style Style, night bool) (top, bottom, cloud Color) {
	base := weatherRole(style.Background, style.Capsule)
	surface := weatherRole(style.Capsule, base)
	primary := weatherRole(style.Accent, style.Secondary)
	secondary := weatherRole(style.Secondary, style.Foreground)
	if night {
		top = LerpColor(base, secondary, .54)
		bottom = LerpColor(surface, base, .28)
	} else {
		top = LerpColor(base, primary, .62)
		bottom = LerpColor(surface, secondary, .62)
	}
	cloud = LerpColor(weatherRole(style.ContainerHighest, surface), style.Foreground, .30)
	return top, bottom, cloud
}

func paintFogHaze(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	base := weatherRole(style.Background, style.Container)
	haze := weatherRole(style.Foreground, style.Track)
	if base.A == 0 || haze.A == 0 {
		return
	}
	time := weatherLoopPhase(phase, spec.Speed)
	ox := float64(scene.X)
	width, height := float64(scene.W), float64(scene.H)
	x0, y0, x1, y1 := c.clip(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for i := 0; i < 3; i++ {
		layer := float64(i)
		x := ox + width*(.20+.31*layer) + math.Sin(2*math.Pi*(time+layer*.27))*width*.20
		y := height*(.33+.23*layer) + math.Sin(2*math.Pi*(time+layer*.41))*height*.035
		band := LerpColor(haze, base, .18+.08*layer)
		bandAlpha := intensity * (.10 + .025*layer) * (.82 + .18*(.5+.5*math.Sin(2*math.Pi*(time+layer*.19))))
		drawWeatherEllipse(c, box, mask, x, y, width*(.46+.06*layer), height*(.075+.012*layer), band, weatherAlpha(band.A, bandAlpha))
	}
	for y := y0; y < y1; y++ {
		v := (float64(y-box.Y) + .5) / float64(box.H)
		for x := x0; x < x1; x++ {
			u := (float64(x-box.X) + .5) / float64(box.W)
			field := .5 + .5*math.Sin(2*math.Pi*(u*1.35+v*2.10+time*.06))
			field += .20 * math.Sin(2*math.Pi*(u*.55-v*.80-time*.035))
			field = clampEffect(field, 0, 1)
			col := LerpColor(base, haze, .25+.35*field)
			col.A = weatherAlpha(col.A, intensity*(.055+.15*field))
			blendWeatherPixel(c, box, mask, x, y, col, 255)
		}
	}
}

func paintRain(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	ink := weatherRole(style.Accent, style.Foreground)
	if ink.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	ox := float64(scene.X)
	width, height := float64(scene.W), float64(scene.H)
	for i := uint64(0); i < weatherRainParticles; i++ {
		xSeed := weatherUnit(spec.Seed, i, 1)
		ySeed := weatherUnit(spec.Seed, i, 2)
		// One band per drop drives length, rate and slant together. Depth read
		// from alpha alone is what makes precipitation look like static noise.
		depth := .40 + float64(i%3)*.30
		fallSpeed := (.72 + weatherUnit(spec.Seed, i, 3)*.46) * depth
		length := 5 + int(weatherUnit(spec.Seed, i, 4)*float64(weatherMaxRainLength-6)*depth)
		slant := 1 + int(weatherUnit(spec.Seed, i, 5)*4*depth)
		life := positiveMod(ySeed+time*fallSpeed/(1+weatherMaxRainLength/height), 1)
		entry := weatherSmoothstep(0, .16, life)
		exit := 1 - weatherSmoothstep(.78, 1, life)
		lifeAlpha := entry * exit
		if lifeAlpha <= 0 {
			continue
		}
		localX := ox + positiveMod(xSeed*width+time*width*.13, width)
		localY := life*(height+float64(length)+1) - float64(length)
		for step := 0; step < length; step++ {
			fraction := float64(step) / float64(max(length-1, 1))
			px := int(math.Floor(ox + positiveMod(localX-ox+fraction*float64(slant), width)))
			py := int(math.Floor(localY + float64(step)))
			coverage := .62 + .30*(1-math.Abs(2*fraction-1))
			localWeatherPixel(c, box, mask, px, py, ink, weatherAlpha(ink.A, intensity*lifeAlpha*depth*(.48+.38*weatherUnit(spec.Seed, i, 6))), coverage)
		}
	}
}

func paintSnow(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64, count int, density float64) {
	if intensity <= 0 || box.W <= 0 || box.H <= 0 || count <= 0 {
		return
	}
	ink := weatherRole(style.Foreground, style.Tertiary)
	if ink.A == 0 {
		return
	}
	time := effectTime(phase, spec.Speed)
	ox := float64(scene.X)
	width, height := float64(scene.W), float64(scene.H)
	for i := uint64(0); i < uint64(count); i++ {
		xSeed := weatherUnit(spec.Seed, i, 11)
		ySeed := weatherUnit(spec.Seed, i, 12)
		depth := .45 + float64(i%3)*.275
		radius := 1 + int(weatherUnit(spec.Seed, i, 13)*float64(weatherMaxSnowRadius)*depth)
		fallSpeed := (.20 + weatherUnit(spec.Seed, i, 14)*.24) * depth
		life := positiveMod(ySeed+time*fallSpeed/(1+2*weatherMaxSnowRadius/height), 1)
		entry := weatherSmoothstep(0, .14, life)
		exit := 1 - weatherSmoothstep(.84, 1, life)
		lifeAlpha := entry * exit
		if lifeAlpha <= 0 {
			continue
		}
		localY := life*(height+2*weatherMaxSnowRadius+1) - weatherMaxSnowRadius
		drift := math.Sin(2*math.Pi*(time*.20+xSeed)) * (1 + weatherUnit(spec.Seed, i, 15)*2)
		localX := ox + positiveMod(xSeed*width+drift, width)
		flakeAlpha := intensity * density * lifeAlpha * depth * (.42 + .42*weatherUnit(spec.Seed, i, 16))
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

func paintLightning(c *Canvas, box, scene ui.Rect, mask *image.Alpha, style Style, spec ui.EffectSpec, phase, intensity float64) {
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
			col.A = weatherAlpha(ink.A, intensity*pulse*(.045+.07*variation))
			blendWeatherPixel(c, box, mask, x, y, col, 255)
		}
	}

	if pulse < .18 {
		return
	}
	// The strike descends from the cloud base rather than from the card's top
	// edge: a bolt that spans the whole card reads as a scratch on the glass.
	baseY := int(float64(scene.H) * weatherLightningOriginY)
	previousX := scene.X + int(weatherUnit(spec.Seed, 91, 1)*float64(max(scene.W, 1)))
	previousY := baseY
	span := max(scene.H-baseY, 1)
	for segment := 1; segment <= weatherLightningSegments; segment++ {
		nextY := baseY + segment*span/weatherLightningSegments
		offset := int((weatherUnit(spec.Seed, uint64(91+segment), 2)*2 - 1) * float64(max(scene.W/8, 2)))
		nextX := clampInt(previousX+offset, scene.X, max(scene.X+scene.W-1, scene.X))
		drawWeatherSegment(c, box, mask, previousX, previousY, nextX, nextY, ink,
			weatherAlpha(ink.A, intensity*pulse*.80), .70)
		glow := LerpColor(ink, style.Foreground, .45)
		drawWeatherStroke(c, box, mask, float64(previousX), float64(previousY), float64(nextX), float64(nextY),
			max(2, float64(min(scene.W, scene.H))*.014), glow, weatherAlpha(glow.A, intensity*pulse*.48))
		drawWeatherSegment(c, box, mask, previousX, previousY, nextX, nextY, style.Foreground,
			weatherAlpha(style.Foreground.A, intensity*pulse*.92), .95)
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

// blendWeatherRow fills one horizontal span with a single colour. Every bound
// blendWeatherPixel checks is invariant across a row, so the sky hoists them
// out of the inner loop and keeps only the mask lookup and the blend. The
// effect loop never settles, so this span runs on every frame a weather
// surface is open; per-pixel revalidation is what put it past the frame cap.
func blendWeatherRow(c *Canvas, box ui.Rect, mask *image.Alpha, y, x0, x1 int, col Color) {
	if c == nil || mask == nil || col.A == 0 ||
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
	if y < box.Y || y >= boxBottom || y < 0 || y >= c.Height {
		return
	}
	lo, hi := max(max(x0, box.X), 0), min(min(x1, boxRight), c.Width)
	if c.restrict.W > 0 && c.restrict.H > 0 {
		restrictRight, rightOK := checkedEffectAdd(c.restrict.X, c.restrict.W)
		restrictBottom, bottomOK := checkedEffectAdd(c.restrict.Y, c.restrict.H)
		if !rightOK || !bottomOK || y < c.restrict.Y || y >= restrictBottom {
			return
		}
		lo, hi = max(lo, c.restrict.X), min(hi, restrictRight)
	}
	b := mask.Bounds()
	my, yOK := checkedEffectAdd(b.Min.Y, y-box.Y)
	if !yOK || my < b.Min.Y || my >= b.Max.Y {
		return
	}
	// Clamp the span so every mask lookup is in bounds without a per-pixel test.
	lo = max(lo, box.X+(b.Min.X-b.Min.X))
	hi = min(hi, box.X+(b.Max.X-b.Min.X))
	if lo >= hi {
		return
	}
	if c.Stride <= 0 || len(c.Pix) < 4 || y > (len(c.Pix)-4)/c.Stride {
		return
	}
	rowOffset := y * c.Stride
	if hi-1 > (len(c.Pix)-rowOffset-4)/4 {
		return
	}
	base := col.premultiply()
	maskRow := (my - b.Min.Y) * mask.Stride
	for x := lo; x < hi; x++ {
		coverage := uint32(mask.Pix[maskRow+(x-box.X)])
		if coverage == 0 {
			continue
		}
		alpha := uint32(col.A) * coverage / 255
		if alpha == 0 {
			continue
		}
		src := base
		if coverage != 255 {
			for i := range src {
				src[i] = byte(uint32(src[i]) * coverage / 255)
			}
		}
		offset := rowOffset + x*4
		blendPixel(c.Pix[offset:offset+4], src, alpha)
	}
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

func weatherLoopPhase(phase, speed float64) float64 {
	if math.IsNaN(phase) || math.IsInf(phase, 0) || math.IsNaN(speed) || math.IsInf(speed, 0) || speed <= 0 {
		return 0
	}
	cycles := math.Round(clampEffect(speed, 0, 4))
	if cycles < 1 {
		cycles = 1
	}
	return positiveMod(positiveMod(phase, 1)*cycles, 1)
}

func weatherSmoothstep(edge0, edge1, value float64) float64 {
	if edge1 <= edge0 {
		if value >= edge1 {
			return 1
		}
		return 0
	}
	t := clampEffect((value-edge0)/(edge1-edge0), 0, 1)
	return t * t * (3 - 2*t)
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
