package shell

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// formatWeather renders one reading and the tone it should paint in.
//
// The three states are deliberately distinct. Nothing fetched yet renders the
// placeholder in the normal tone, because nothing has gone wrong. A fetch that
// has never succeeded renders an error. A reading whose fetch later began
// failing keeps its value and its normal tone, and shows its age: an aged
// temperature is information, a blank widget is not.
func formatWeather(item config.Item, reading services.Reading) (string, ui.Tone) {
	if !reading.Observed {
		if reading.FailedSince.IsZero() {
			return noWorkspace, ui.ToneNormal
		}
		return "weather unavailable", ui.ToneError
	}

	icon := render.IconRune(reading.Code)
	text := fmt.Sprintf("%c %.0f%s", icon, reading.Temperature, unitSuffix(reading.Unit))
	if item.ShowCondition {
		text += " " + render.WeatherCondition(reading.Code)
	}
	if reading.Stale() {
		text += " (" + humaniseAge(time.Since(reading.FetchedAt)) + ")"
	}
	return text, ui.ToneNormal
}

func unitSuffix(u services.Unit) string {
	if u == services.UnitFahrenheit {
		return "°F"
	}
	return "°C"
}

// humaniseAge renders an age at one significant unit. A bar has no room for
// "1h32m14s", and the reader only needs to know roughly how old this is.
func humaniseAge(age time.Duration) string {
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours())/24)
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return "now"
}
