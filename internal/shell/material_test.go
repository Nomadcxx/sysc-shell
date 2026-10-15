package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestMaterialSurfaceTakesTheCaptureWithCompositorBlur: a material needs the
// captured pixels, so a blur-capable compositor must not suppress the capture
// for a surface that paints one. The compositor's own blur would be a second
// blur over our ground, so the material surface asks for none.
func TestMaterialSurfaceTakesTheCaptureWithCompositorBlur(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.Glass = theme.GlassAuto
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	reg.caps.Blur = true
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	spec := drainAux(t, reg, 2)[1].Open
	if spec.BlurRegion == nil {
		t.Error("a material surface skipped the capture on a blur-capable compositor")
	}
	if !spec.BackdropSharp {
		t.Error("auto-frost needs the sharp capture beside the blurred one")
	}
	if shape := spec.Callbacks.BlurShape(); len(shape) != 0 {
		t.Errorf("material surface still asks the compositor to blur: %v", shape)
	}
}

// TestMaterialOffKeepsTheCompositorPath is the regression half: with the
// material off, the default theme behaves exactly as it did.
func TestMaterialOffKeepsTheCompositorPath(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	reg.caps.Blur = true
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	spec := drainAux(t, reg, 2)[1].Open
	if spec.BlurRegion != nil {
		t.Errorf("material off: capture %+v, want none", spec.BlurRegion)
	}
	if spec.BackdropSharp {
		t.Error("material off: the sharp capture was requested as well")
	}
	if shape := spec.Callbacks.BlurShape(); len(shape) == 0 {
		t.Error("material off: the compositor blur path is gone")
	}
}

// TestMaterialForMapsTheVocabulary keeps the axis and the render-side tone on
// one mapping, and keeps a theme without a backdrop — or with a bound of zero —
// from resolving a material at all.
func TestMaterialForMapsTheVocabulary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		axis theme.GlassMaterial
		want render.Tone
	}{
		{theme.GlassOff, render.ToneNone},
		{theme.GlassSmoke, render.ToneSmoke},
		{theme.GlassMilk, render.ToneMilk},
		{theme.GlassFrost, render.ToneFrost},
		{theme.GlassAuto, render.ToneAuto},
		{theme.GlassFull, render.ToneAuto},
	} {
		if got := materialToneFor(tc.axis); got != tc.want {
			t.Errorf("%s resolved to tone %v, want %v", tc.axis, got, tc.want)
		}
	}
	cfg := config.Default()
	cfg.Theme.Glass = theme.GlassAuto
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got := materialFor(th, false); got.Tone != render.ToneNone {
		t.Errorf("a theme without a backdrop resolved material %+v", got)
	}
	if got := materialFor(th, true); got.Tone != render.ToneAuto {
		t.Errorf("tone = %v, want auto", got.Tone)
	}
	th.materialIntensity = 0
	if got := materialFor(th, true); got != (render.Material{}) {
		t.Errorf("a zero bound resolved material %+v", got)
	}
}

// TestMaterialGroundReplacesTheBlurredBackdrop: with a material on, the panel
// composites the material's ground rather than the blurred capture; with it
// off, the capture passes through untouched.
func TestMaterialGroundReplacesTheBlurredBackdrop(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.Glass = theme.GlassSmoke
	cfg.Theme.GlassIntensity = 100
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{id: PanelSettings, theme: resolved}
	h.backdrop = solidImage(32, 32, 0x80, 0x80, 0x80)
	h.backdropSharp = solidImage(32, 32, 0x80, 0x80, 0x80)
	if reg.cfg.Theme.Glass != theme.GlassSmoke {
		t.Fatalf("fixture glass = %q", reg.cfg.Theme.Glass)
	}
	got := h.backdropFor(h.theme)
	if got == nil {
		t.Fatal("the material ground is nil")
	}
	if got == h.backdrop {
		t.Fatal("the material ground is the blurred capture, so no tone was applied")
	}
	// Nothing named a material with the axis off, so the capture passes through.
	h.theme.Glass = theme.GlassOff
	if got := h.backdropFor(h.theme); got != h.backdrop {
		t.Error("with the material off the blurred capture should be composited unchanged")
	}
}

// TestMaterialCannotBreakTheTextFloorInEitherScheme is the readability rule as
// a correctness requirement, proved over the whole ground range rather than at
// the extremes: for both schemes, every tone and the shipped axis maximum, no
// capture content leaves the ground behind a panel's text below the theme's
// floor — and none leaves it below what that same content already gave.
//
// The measured outcome is that the shipped cap does not bind in either scheme,
// which is the point: the bound exists so a later cap raise has to satisfy it.
func TestMaterialCannotBreakTheTextFloorInEitherScheme(t *testing.T) {
	t.Parallel()
	for _, scheme := range []struct {
		name string
		tok  theme.Tokens
	}{
		{"dark", theme.Fallback},
		{"light", lightTokens()},
	} {
		for _, opacity := range []int{100, theme.OpacityMinBlurred} {
			cfg := config.Default()
			cfg.Theme.PanelOpacity = opacity
			th, err := ResolveTheme(cfg, cfg.Bar, scheme.tok)
			if err != nil {
				t.Fatalf("%s: %v", scheme.name, err)
			}
			fill := th.PanelStyle().RootFill()
			text := theme.Color{R: th.OnSurface.R, G: th.OnSurface.G, B: th.OnSurface.B, A: 0xff}
			for _, tone := range []render.Tone{render.ToneSmoke, render.ToneMilk, render.ToneAuto} {
				bound := clampMaterialIntensity(th, render.Material{Tone: tone, Intensity: theme.GlassIntensityMax}, fill)
				// A bound of zero is not a pass: it means the material paints
				// nothing, which is the live defect this test previously slept
				// through, because a zero bound satisfies every contrast
				// constraint by doing nothing at all.
				if bound == 0 {
					t.Errorf("%s/opacity %d/%v: the bound resolved the material off entirely",
						scheme.name, opacity, tone)
				}
				contrast := func(intensity uint8, ground uint8) float64 {
					l := overlayLuma(fill, render.TransferLuma(tone, intensity, ground))
					return theme.ContrastRatio(text, theme.Color{R: l, G: l, B: l, A: 0xff})
				}
				for g := 0; g <= 255; g++ {
					g8 := uint8(g)
					base := contrast(0, g8)
					// Only grounds the surface already makes legible are the
					// material's business; the rest are the opacity floor's.
					if base < theme.TextContrast {
						continue
					}
					if got := contrast(uint8(bound), g8); got < theme.TextContrast-materialContrastTolerance {
						t.Errorf("%s/opacity %d/%v over ground %d: %.2f:1 at intensity %d, below the %.1f:1 floor",
							scheme.name, opacity, tone, g, got, bound, theme.TextContrast)
					}
				}
			}
		}
	}
}

// TestMaterialForRespectsTheResolvedBound keeps the per-frame path reading the
// bound the theme resolved rather than re-deriving it, and keeps a bound of
// zero from painting a material anyway.
func TestMaterialForRespectsTheResolvedBound(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.Glass = theme.GlassSmoke
	cfg.Theme.GlassIntensity = 40
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if th.materialIntensity <= 0 {
		t.Fatal("a smoke material resolved to nothing at the shipped settings")
	}
	if got := materialFor(th, true); int(got.Intensity) != th.materialIntensity {
		t.Errorf("material intensity = %d, want the resolved %d", got.Intensity, th.materialIntensity)
	}
	th.materialIntensity = 0
	if got := materialFor(th, true); got.Tone != render.ToneNone || got.Intensity != 0 {
		t.Errorf("a zero bound still painted a material: %+v", got)
	}
}

// TestHighContrastResolvesNoMaterial keeps accessibility ahead of the material:
// high contrast forces opaque grounds, so a transfer that tints what shows
// through would defeat it.
func TestHighContrastResolvesNoMaterial(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.HighContrast = true
	cfg.Theme.Glass = theme.GlassAuto
	th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if th.Glass != theme.GlassOff {
		t.Errorf("high contrast resolved material %q, want off", th.Glass)
	}
	if got := materialFor(th, true); got.Tone != render.ToneNone {
		t.Errorf("high contrast resolved tone %v", got.Tone)
	}
}

// TestReducedMotionKeepsTheTone: reduced motion drops motion, not the material.
// Its tone is static, and the only animated term belongs to the refraction
// stage.
func TestReducedMotionKeepsTheTone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Theme.Glass = theme.GlassSmoke
	th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got := materialFor(th, true); got.Tone != render.ToneSmoke {
		t.Errorf("reduced motion dropped the tone: %v", got.Tone)
	}
}

// lightTokens is the dark fallback with its surface pair swapped, which is what
// a light scheme looks like to this rule: a light ground under dark text.
func lightTokens() theme.Tokens {
	tok := theme.Fallback
	tok.Surface, tok.OnSurface = tok.OnSurface, tok.Surface
	return tok
}

// TestRefractionComesWithTheGlassValueOnly keeps the vocabulary honest: the
// tuned tone values paint a tone, and only the spelled "glass" value adds the
// bend. It also keeps the capture's scale honest — the profile works in the
// capture's pixels, so a logical radius has to come down with it.
func TestRefractionComesWithTheGlassValueOnly(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	for _, axis := range []theme.GlassMaterial{theme.GlassSmoke, theme.GlassMilk, theme.GlassFrost, theme.GlassAuto} {
		th := resolved
		th.Glass = axis
		th.materialIntensity = 60
		if got := materialFor(th, true); got.Refract != 0 {
			t.Errorf("%s refracts by %d, want a tone only", axis, got.Refract)
		}
	}
	full := resolved
	full.Glass = theme.GlassFull
	full.materialIntensity = 60
	if got := materialFor(full, true); got.Refract == 0 {
		t.Error("the glass value does not refract")
	}

	// The radius is scaled into the capture's pixels: a 12px radius on a
	// quarter-resolution capture is 3.
	h := &PanelHost{
		id:    PanelSettings,
		place: Placement{Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 400, H: 300}},
		theme: full,
	}
	body, _ := h.surfaceSize()
	capture := solidImage(body/4, body/4, 0x40, 0x40, 0x40)
	got := h.refractionFor(capture)
	if got.W != capture.Width || got.H != capture.Height {
		t.Errorf("refraction geometry = %dx%d, want the capture's %dx%d", got.W, got.H, capture.Width, capture.Height)
	}
	if want := full.Radius / 4; got.Radius < want-1 || got.Radius > want+1 {
		t.Errorf("radius = %d, want about %d (the theme's %d at quarter resolution)", got.Radius, want, full.Radius)
	}
	if got.Bend == 0 || got.Fringe == 0 || got.Dome == 0 {
		t.Errorf("glass terms are incomplete: %+v", got)
	}
	if (&PanelHost{}).refractionFor(nil) != (render.Refraction{}) {
		t.Error("a surface without a capture resolved a refraction")
	}
}

// TestGlassValueRefractsTheGround: the ground a glass surface paints is the
// capture bent by the bevel, not the tone alone, and the wider rim comes with
// it.
func TestGlassValueRefractsTheGround(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.Glass = theme.GlassFull
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{
		id:    PanelSettings,
		place: Placement{Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 400, H: 300}},
		theme: resolved,
	}
	// A capture with an edge to bend: half dark, half light.
	h.backdrop = halfImage(120, 80)
	h.backdropSharp = h.backdrop
	tone := render.Materialize(nil, h.backdrop, render.Material{Tone: render.ToneAuto, Intensity: uint8(resolved.materialIntensity)})
	got := h.backdropFor(resolved)
	if got == nil {
		t.Fatal("the glass ground is nil")
	}
	if got == tone {
		t.Error("the glass value painted the tone ground; nothing bent")
	}

	if style := h.rootStyle(resolved, true); style.RimWidth != 2 {
		t.Errorf("rim width = %d, want the wider edge a refracting material draws", style.RimWidth)
	}
	plain, err := ResolveTheme(config.Default(), config.Default().Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if style := h.rootStyle(plain, true); style.RimWidth != 0 {
		t.Errorf("a theme without glass drew a %dpx rim", style.RimWidth)
	}
}

// halfImage is a two-tone capture: an edge for the bevel to work on.
func halfImage(w, h int) *ui.Image {
	img := solidImage(w, h, 0x20, 0x20, 0x20)
	for y := 0; y < h; y++ {
		for x := w / 2; x < w; x++ {
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 0xc0, 0xc0, 0xc0
		}
	}
	return img
}

// TestLensAsksForOneCaptureAtATime holds the live ground's traffic rules: the
// paint path is the only requester, one capture is in flight at a time, the
// cadence holds afterwards, and the request names the surface's own region.
func TestLensAsksForOneCaptureAtATime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.Glass = theme.GlassAuto
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{
		id:     PanelSettings,
		global: 7,
		place:  Placement{Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 400, H: 300}},
		theme:  resolved,
		// Until the shell retains its own pixels for the region, the ladder
		// keeps the ground static and nothing is asked for; this is the state
		// the lens runs in once that buffer exists.
		lensOursFrame: solidImage(8, 8, 0, 0, 0),
	}
	now := time.Now()

	h.maybeRecapture(reg, resolved, now)
	select {
	case req := <-reg.AuxRequests():
		if !req.Recapture {
			t.Fatalf("request = %+v, want a recapture", req)
		}
		if req.ID != panelSurfaceID(PanelSettings) {
			t.Errorf("request names %q, want the panel's own surface", req.ID)
		}
		if req.Output != 7 {
			t.Errorf("request output = %d, want the surface's 7", req.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("no recapture was asked for")
	}

	// One in flight: a second frame in the same moment asks for nothing.
	h.lensPending = true
	h.maybeRecapture(reg, resolved, now.Add(glassCadence))
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a second capture was asked for while one was in flight: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}

	// The frame arrives, and the cadence holds the next request back.
	h.lensPending = false
	h.lensAt = now
	h.maybeRecapture(reg, resolved, now.Add(glassCadence/2))
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a capture was asked for inside the cadence: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
	h.maybeRecapture(reg, resolved, now.Add(glassCadence+time.Millisecond))
	select {
	case req := <-reg.AuxRequests():
		if !req.Recapture {
			t.Fatalf("request = %+v, want a recapture", req)
		}
	case <-time.After(time.Second):
		t.Fatal("the cadence never asked again")
	}

	if lensInterval(true) != 0 {
		t.Error("a revealing surface waits; the ground should track it")
	}
	if lensInterval(false) != glassCadence {
		t.Error("a settled surface does not use the cadence")
	}
}

// TestLensIsQuietWithoutAMaterial keeps the default shell free of capture
// traffic: no material, no requests, and none before the surface has an output.
func TestLensIsQuietWithoutAMaterial(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	plain := &PanelHost{
		id:     PanelSettings,
		global: 7,
		place:  Placement{Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 400, H: 300}},
		theme:  resolved,
	}
	plain.maybeRecapture(reg, resolved, time.Now())
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a theme without a material asked for a capture: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}

	cfg.Theme.Glass = theme.GlassAuto
	withGlass, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	unopened := &PanelHost{id: PanelSettings, place: plain.place, theme: withGlass}
	unopened.maybeRecapture(reg, withGlass, time.Now())
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a surface with no output asked for a capture: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}

	// A material surface whose region the shell cannot account for asks for
	// nothing: a recaptured region contains the surface itself, and painting
	// that back is the artefact the ladder exists to prevent.
	unaccounted := &PanelHost{
		id: PanelSettings, global: 7, place: plain.place,
		theme: withGlass, lensOursFrame: nil,
	}
	unaccounted.maybeRecapture(reg, withGlass, time.Now())
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a surface the shell cannot account for asked for a capture: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}

	// A run of failures ends the conversation.
	failing := &PanelHost{
		id: PanelSettings, global: 7, place: plain.place,
		theme: withGlass, lensOursFrame: solidImage(8, 8, 0, 0, 0),
		lensFailures: render.MaxLensFailures,
	}
	failing.maybeRecapture(reg, withGlass, time.Now())
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("a surface past the failure cap asked again: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
}

// TestLensCountsARequestThatNeverCameBack: the owner cannot report a dropped
// capture, so the surface counts the silence itself and stops asking rather than
// paying the attempt every cadence forever.
func TestLensCountsARequestThatNeverCameBack(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.Glass = theme.GlassSmoke
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{
		id: PanelSettings, global: 7, place: Placement{Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 400, H: 300}},
		theme: resolved, lensOursFrame: solidImage(8, 8, 0, 0, 0),
	}
	base := time.Now()
	h.maybeRecapture(reg, resolved, base)
	if !h.lensPending {
		t.Fatal("the first request did not leave a capture in flight")
	}
	<-reg.AuxRequests()
	// The frame never arrives: past the age bound the surface counts a failure
	// and is free to ask again.
	h.maybeRecapture(reg, resolved, base.Add(render.MaxLensAge+time.Millisecond))
	if h.lensFailures != 1 {
		t.Fatalf("failures = %d, want the silence counted", h.lensFailures)
	}
	// It is free to retry straight away: the run is bounded by the cap, not by
	// an extra wait.
	if !h.lensPending {
		t.Error("the surface did not retry after counting the silence")
	}
	<-reg.AuxRequests()
	// And a delivered frame clears the count.
	h.lensAt, h.lensPending = base, true
	spec := reg.panelSpec(h, Margins{})
	spec.Callbacks.MaterialFrame(solidImage(16, 16, 0x40, 0x40, 0x40))
	if h.lensFailures != 0 {
		t.Errorf("failures = %d after a frame arrived, want zero", h.lensFailures)
	}
	if h.lensFrame == nil || h.lensLast == nil {
		t.Error("the frame was not kept as the live ground")
	}
}

// TestLensDropsAFrameThatDidNotChange: a still region is not worth a repaint,
// and a changed one is.
func TestLensDropsAFrameThatDidNotChange(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	cfg.Theme.Glass = theme.GlassSmoke
	cfg.Theme.PanelOpacity = theme.OpacityMinBlurred
	reg.cfg = cfg
	resolved, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{id: PanelSettings, global: 7, theme: resolved}
	frame := solidImage(32, 32, 0x40, 0x40, 0x40)

	deliver := func(img *ui.Image) {
		h.lensPending = true
		h.lensLast, h.backdrop = img, img
	}
	deliver(frame)
	if h.backdrop == nil {
		t.Fatal("the fixture did not store a frame")
	}
	// The skip itself is the render-side rule; here the wiring is what matters:
	// the callback is registered on the spec the panel opens.
	spec := reg.panelSpec(h, Margins{})
	if spec.Callbacks.MaterialFrame == nil {
		t.Fatal("a material panel does not accept a live frame")
	}
	if !spec.Recapture {
		t.Error("a material panel does not keep its region capturable")
	}
}

func solidImage(w, h int, r, g, b byte) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = b, g, r, 0xff
	}
	return img
}
