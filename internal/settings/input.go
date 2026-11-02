package settings

import (
	"fmt"
	"math"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func (r *Registry) addInputEntries(o options) {
	for _, device := range []string{"touchpad", "mouse"} {
		if device == "touchpad" && o.hideTouchpad {
			continue
		}
		group := "Mouse"
		effective := o.input.Mouse
		pointer := func(c *config.Config) *config.Pointer { return &c.Input.Mouse }
		if device == "touchpad" {
			group, effective = "Touchpad", o.input.Touchpad
			pointer = func(c *config.Config) *config.Pointer { return &c.Input.Touchpad }
		}
		add := func(e Entry) {
			e.Describe = inputDescriptions[e.Path]
			e.Path = "input." + device + "." + e.Path
			e.Section, e.Page, e.Group = "Session", "Mouse & touchpad", group
			e.Disabled = o.inputBlocked
			if e.Disabled != "" {
				e.Describe = e.Disabled
			}
			set := e.Set
			e.Set = write(func(c *config.Config, value string) error {
				if o.inputBlocked != "" {
					return fmt.Errorf("%s", o.inputBlocked)
				}
				candidate := c.Clone()
				if err := set(&candidate, value); err != nil {
					return err
				}
				if err := config.ValidateInput(candidate.Input); err != nil {
					return err
				}
				c.Input = candidate.Input
				return nil
			})
			r.entries = append(r.entries, e)
		}
		boolean := func(path, label string, field func(*config.Pointer) **bool, fallback bool) {
			add(Entry{Path: path, Label: label, Kind: KindBool,
				Get: func(c config.Config) string {
					p := *field(pointer(&c))
					if p == nil {
						p = *field(&effective)
					}
					if p != nil {
						return strconv.FormatBool(*p)
					}
					return strconv.FormatBool(fallback)
				},
				Set: func(c *config.Config, value string) error {
					v, err := strconv.ParseBool(value)
					if err == nil {
						*field(pointer(c)) = &v
					}
					return err
				},
			})
		}
		percent := func(path, label string, field func(*config.Pointer) **float64, min, max int, fallback float64) {
			add(Entry{Path: path, Label: label, Kind: KindInt, Present: PresentSlider, Unit: "%", Min: min, Max: max,
				Get: func(c config.Config) string {
					p := *field(pointer(&c))
					if p == nil {
						p = *field(&effective)
					}
					v := fallback
					if p != nil {
						v = *p
					}
					return strconv.Itoa(int(math.Round(v * 100)))
				},
				Set: func(c *config.Config, value string) error {
					n, err := strconv.Atoi(value)
					if err != nil {
						return err
					}
					if n < min || n > max {
						return fmt.Errorf("%s must be between %d and %d", path, min, max)
					}
					v := float64(n) / 100
					*field(pointer(c)) = &v
					return nil
				},
			})
		}
		enum := func(path, label string, field func(*config.Pointer) **string, values []string, fallback string) {
			add(Entry{Path: path, Label: label, Kind: KindEnum, Options: values,
				Get: func(c config.Config) string {
					p := *field(pointer(&c))
					if p == nil {
						p = *field(&effective)
					}
					if p != nil {
						return *p
					}
					return fallback
				},
				Set: func(c *config.Config, value string) error {
					for _, v := range values {
						if value == v {
							*field(pointer(c)) = &value
							return nil
						}
					}
					return fmt.Errorf("invalid %s: %q", path, value)
				},
			})
		}
		boolean("natural-scroll", "Natural scrolling", func(p *config.Pointer) **bool { return &p.NaturalScroll }, false)
		percent("accel-speed", "Pointer speed", func(p *config.Pointer) **float64 { return &p.AccelSpeed }, -100, 100, 0)
		enum("accel-profile", "Acceleration profile", func(p *config.Pointer) **string { return &p.AccelProfile }, []string{"adaptive", "flat"}, "adaptive")
		percent("scroll-factor", "Scroll speed", func(p *config.Pointer) **float64 { return &p.ScrollFactor }, 0, 10000, 1)
		if device == "mouse" {
			boolean("left-handed", "Left handed", func(p *config.Pointer) **bool { return &p.LeftHanded }, false)
		} else {
			boolean("tap", "Tap to click", func(p *config.Pointer) **bool { return &p.Tap }, false)
			boolean("dwt", "Disable while typing", func(p *config.Pointer) **bool { return &p.DWT }, false)
			enum("scroll-method", "Scroll method", func(p *config.Pointer) **string { return &p.ScrollMethod }, []string{"two-finger", "edge"}, "two-finger")
			enum("click-method", "Click method", func(p *config.Pointer) **string { return &p.ClickMethod }, []string{"clickfinger", "button-areas"}, "button-areas")
			boolean("disabled-on-external-mouse", "Off when a mouse is connected", func(p *config.Pointer) **bool { return &p.DisabledOnExternalMouse }, false)
		}
	}
}

var inputDescriptions = map[string]string{
	"natural-scroll":             "Move content in the direction of the scroll gesture.",
	"accel-speed":                "Pointer acceleration from −100% to 100%; 0 uses the default speed.",
	"accel-profile":              "Adaptive responds to movement speed; flat keeps a constant ratio.",
	"scroll-factor":              "Scale scrolling; 100% keeps the configured device speed.",
	"left-handed":                "Swap the primary and secondary mouse buttons.",
	"tap":                        "Click by tapping the touchpad.",
	"dwt":                        "Ignore touchpad input while typing on the keyboard.",
	"scroll-method":              "Scroll with two fingers or along the touchpad edge.",
	"click-method":               "Choose whether finger count or the pressed area determines the button.",
	"disabled-on-external-mouse": "Disable the touchpad while an external mouse is connected.",
}
