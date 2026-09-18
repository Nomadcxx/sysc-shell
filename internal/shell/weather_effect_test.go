package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWeatherEffectSpecMapsWeatherCategories(t *testing.T) {
	day := true
	cases := []struct {
		name    string
		code    int
		variant ui.EffectVariant
		isDay   *bool
		night   bool
	}{
		{name: "clear day", code: 0, variant: ui.WeatherClear, isDay: &day},
		{name: "clear night", code: 0, variant: ui.WeatherClear, isDay: func() *bool { v := false; return &v }(), night: true},
		{name: "partly cloudy", code: 2, variant: ui.WeatherPartlyCloudy, isDay: &day},
		{name: "cloudy", code: 3, variant: ui.WeatherCloudy, isDay: &day},
		{name: "fog", code: 45, variant: ui.WeatherFog, isDay: &day},
		{name: "rain", code: 61, variant: ui.WeatherRain, isDay: &day},
		{name: "snow", code: 71, variant: ui.WeatherSnow, isDay: &day},
		{name: "heavy snow", code: 75, variant: ui.WeatherHeavySnow, isDay: &day},
		{name: "thunderstorm", code: 95, variant: ui.WeatherThunderstorm, isDay: &day},
		{name: "unknown falls back to cloudy", code: 44, variant: ui.WeatherCloudy, isDay: &day},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			reading := services.Reading{Observed: true, Code: tt.code, IsDay: tt.isDay}
			spec, ok := weatherEffectSpec(reading)
			if !ok {
				t.Fatal("observed reading did not produce an effect")
			}
			if spec.Program != ui.EffectWeather || spec.Variant != tt.variant {
				t.Fatalf("spec = %+v, want weather variant %d", spec, tt.variant)
			}
			if spec.Night != tt.night {
				t.Fatalf("spec night = %v, want %v", spec.Night, tt.night)
			}
			if err := spec.Validate(); err != nil {
				t.Fatalf("effect spec is invalid: %v", err)
			}
		})
	}
}

func TestWeatherEffectStateKeepsStaleAndSkipsStaticReadings(t *testing.T) {
	observed := observedWeather()
	observed.FetchedAt = time.Now().Add(-90 * time.Minute)
	observed.FailedSince = time.Now().Add(-30 * time.Minute)
	if node := weatherEffectNode(observed, "weather:hero", 0); node == nil {
		t.Fatal("stale observed reading lost its animated effect")
	}
	if node := weatherEffectNode(services.Reading{}, "weather:hero", 0); node != nil {
		t.Fatal("placeholder reading produced an effect")
	}
	if node := weatherEffectNode(services.Reading{FailedSince: time.Now()}, "weather:hero", 0); node != nil {
		t.Fatal("error reading produced an effect")
	}
}

func TestWeatherEffectWrapperPreservesCardAndPlacesEffectFirst(t *testing.T) {
	content := &ui.Node{Kind: ui.KindColumn, Name: "Weather content", Role: "group"}
	card := &ui.Node{
		Kind: ui.KindCapsule, Width: 320, Height: 180, Padding: 13,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Action: "weather-open", Name: "Weather", Role: "region",
		Children: []*ui.Node{content},
	}
	reading := observedWeather()

	got := weatherCardWithEffect(card, reading, "weather:hero", 0)
	if got != card {
		t.Fatal("weather wrapper replaced the card node")
	}
	if got.Width != 320 || got.Height != 180 || got.Padding != 13 ||
		got.Fill != ui.FillContainerHigh || got.Shape != ui.ShapeCard ||
		got.Action != "weather-open" || got.Name != "Weather" || got.Role != "region" {
		t.Fatalf("card chrome or accessibility changed: %+v", got)
	}
	if len(got.Children) != 1 || got.Children[0].Kind != ui.KindStack {
		t.Fatalf("card content = %+v, want one stack", got.Children)
	}
	stack := got.Children[0]
	if len(stack.Children) != 3 {
		t.Fatalf("weather stack has %d children, want effect, scrim, foreground", len(stack.Children))
	}
	if stack.Children[0].Kind != ui.KindEffect || stack.Children[0].Key != "weather:hero" {
		t.Fatalf("stack effect = %+v, want first stable effect layer", stack.Children[0])
	}
	if stack.Children[1].Kind != ui.KindCapsule || stack.Children[1].Fill != ui.FillScrim {
		t.Fatalf("stack scrim = %+v, want quiet scrim", stack.Children[1])
	}
	if stack.Children[2] != content {
		t.Fatal("stack did not retain the original foreground content")
	}
}

func TestWeatherHeroUsesTheWeatherEffect(t *testing.T) {
	reading := observedWeather()
	hero := weatherHero(reading, "Brisbane", DefaultTheme().Metrics, 346)
	effect := firstWeatherEffect(hero)
	if effect == nil || effect.Key != "weather:hero" {
		t.Fatalf("hero effect = %+v, want the stable hero effect", effect)
	}
	want, ok := weatherEffectSpec(reading)
	if !ok || effect.Effect != want {
		t.Fatalf("hero effect spec = %+v, want %+v", effect.Effect, want)
	}
}

// The mark keeps the upper band and the reading is centred in the lower one,
// so the two share a centreline without the reading sitting on the form.
func TestWeatherHeroSeatsTheReadingBelowTheMark(t *testing.T) {
	r := &Registry{reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	root := weatherTree(r, h)
	if err := ui.LayoutColumn(root, ui.Rect{W: 460, H: 560}, h.measureText()); err != nil {
		t.Fatalf("weather tree does not lay out: %v", err)
	}

	hero := root.Children[1].Children[0]
	foreground := hero.Children[0].Children[2]
	group := foreground.Children[0]
	band := group.Children[1]
	stack := band.Children[0]
	if stack.Bounds.H <= 0 {
		t.Fatal("reading stack has no height")
	}
	// The reading sits in the lower band, clear of the mark's.
	if stack.Bounds.Y <= group.Bounds.Y+group.Bounds.H/4 {
		t.Fatalf("reading y=%d is not below the mark band of group %+v", stack.Bounds.Y, group.Bounds)
	}
	// And it is centred within that band.
	above := stack.Bounds.Y - band.Bounds.Y
	below := (band.Bounds.Y + band.Bounds.H) - (stack.Bounds.Y + stack.Bounds.H)
	if above <= 0 {
		t.Fatalf("reading starts at the top of its band (slack above = %d)", above)
	}
	if diff := above - below; diff > 1 || diff < -1 {
		t.Fatalf("reading is not centred in its band: %d above, %d below", above, below)
	}
}

// A stale reading still says so in full; only the fresh form is abbreviated to
// its time, because the panel is too narrow for both.
func TestWeatherMetaLineKeepsTheStaleWarning(t *testing.T) {
	// Staleness is a failed refresh, not age: Reading.Stale reads FailedSince.
	stale := observedWeather()
	stale.FetchedAt = time.Now().Add(-3 * time.Hour)
	stale.FailedSince = time.Now().Add(-2 * time.Hour)
	if got := weatherMetaLine(stale, "Lisbon"); !strings.Contains(got, "Stale") {
		t.Fatalf("stale meta line = %q, want the stale warning", got)
	}
	fresh := observedWeather()
	if got := weatherMetaLine(fresh, "Lisbon"); strings.Contains(got, "Stale") {
		t.Fatalf("fresh meta line = %q, want no stale warning", got)
	}
}

func TestControlCentreWeatherUsesTheSameWeatherEffect(t *testing.T) {
	reading := observedWeather()
	r := &Registry{reading: reading}
	h := &PanelHost{id: PanelControlCenter, section: "weather", theme: DefaultTheme()}
	page := ccWeather(r, h)
	effect := firstWeatherEffect(page.Children[0])
	if effect == nil || effect.Key != "weather:today" {
		t.Fatalf("Today effect = %+v, want the stable Today effect", effect)
	}
	want, ok := weatherEffectSpec(reading)
	want.SceneBias = -1 // the Control Centre card is wide enough to split
	if !ok || effect.Effect != want {
		t.Fatalf("Today effect spec = %+v, want %+v", effect.Effect, want)
	}
	if firstWeatherEffect(page.Children[1]) != nil {
		t.Fatal("forecast strip unexpectedly became animated")
	}
}

func firstWeatherEffect(root *ui.Node) *ui.Node {
	var effect *ui.Node
	walkNodes(root, func(n *ui.Node) {
		if effect == nil && n.Kind == ui.KindEffect {
			effect = n
		}
	})
	return effect
}

func TestWeatherPanelFitsLaptopScale(t *testing.T) {
	r := &Registry{reading: observedWeather()}
	h := &PanelHost{id: PanelWeather, scale120: 150, theme: DefaultTheme()}
	root := weatherTree(r, h)
	if err := ui.LayoutColumn(root, ui.Rect{W: 460, H: 560}, h.measureText()); err != nil {
		t.Fatalf("weather panel does not fit 460x560 at scale 1.25: %v", err)
	}
}
