package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

func nightLightSchedule(cfg config.NightLight) services.NightLightSchedule {
	start, _ := time.Parse("15:04", cfg.Start)
	end, _ := time.Parse("15:04", cfg.End)
	return services.NightLightSchedule{
		Mode:       cfg.Mode,
		NightK:     cfg.NightKelvin,
		DayK:       cfg.DayKelvin,
		Transition: time.Duration(cfg.TransitionMinutes) * time.Minute,
		Start:      time.Duration(start.Hour())*time.Hour + time.Duration(start.Minute())*time.Minute,
		End:        time.Duration(end.Hour())*time.Hour + time.Duration(end.Minute())*time.Minute,
	}
}

func (r *Registry) syncNightLightWeatherLeaseLocked(cfg config.Config) {
	needsCity := cfg.NightLight.Mode == config.NightLightModeSunset && cfg.Weather.City != ""
	if !needsCity {
		if r.nightLightWeatherLease != nil {
			r.nightLightWeatherLease.Release()
			r.nightLightWeatherLease = nil
		}
		r.nightLightWeatherInterval = 0
		return
	}
	interval := cfg.Weather.Interval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if r.nightLightWeatherLease != nil && r.nightLightWeatherInterval == interval {
		return
	}
	if r.nightLightWeatherLease != nil {
		r.nightLightWeatherLease.Release()
		r.nightLightWeatherLease = nil
		r.nightLightWeatherInterval = 0
	}
	if runningAsTest() {
		return
	}
	lease, err := r.weather.Acquire(interval)
	if err != nil {
		log.Printf("night light: weather location lease: %v", err)
		return
	}
	r.nightLightWeatherLease = lease
	r.nightLightWeatherInterval = interval
}

// NightLight is the schedule service shared with the control centre and IPC.
func (r *Registry) NightLight() *services.NightLightService { return r.nightLight }

// GammaRequests carries colour-temperature changes to the Wayland owner.
func (r *Registry) GammaRequests() <-chan wayland.GammaRequest {
	if r.nightLight == nil {
		return nil
	}
	return r.nightLight.Requests()
}

// GammaEvents receives per-output gamma state from the Wayland owner.
func (r *Registry) GammaEvents() <-chan wayland.GammaEvent { return r.gammaEvents }

// GammaEventSink gives the Wayland owner the send side of the status channel.
func (r *Registry) GammaEventSink() chan<- wayland.GammaEvent { return r.gammaEvents }

// ApplyGammaEvent applies one immutable output status to the policy service.
func (r *Registry) ApplyGammaEvent(ev wayland.GammaEvent) {
	if r.nightLight != nil {
		r.nightLight.GammaEvent(ev)
	}
}

// NightLightCall answers nightlight.* IPC methods. Persistent temperature
// changes go through the same Settings entry and config reload as the pane.
func (r *Registry) NightLightCall(method string, params json.RawMessage) (map[string]any, error) {
	if r.nightLight == nil {
		return nil, errors.New("night light handler unset")
	}
	decode := func(dst any) error {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, dst); err != nil {
			return errors.New("malformed params")
		}
		return nil
	}
	switch method {
	case "nightlight.status":
		var p struct{}
		if err := decode(&p); err != nil {
			return nil, err
		}
		state := r.nightLight.State()
		next := ""
		if !state.NextChange.IsZero() {
			next = state.NextChange.Format(time.RFC3339)
		}
		return map[string]any{
			"mode": state.Mode, "active": state.Active,
			"kelvin": state.Kelvin, "target": state.Target,
			"next_change": next, "available": state.Available,
			"reason": state.Reason, "override": state.Override,
		}, nil
	case "nightlight.set":
		var p struct {
			On *bool `json:"on"`
		}
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.On == nil {
			return nil, errors.New("nightlight.set needs on")
		}
		r.nightLight.SetOverride(*p.On)
		return map[string]any{"on": *p.On}, nil
	case "nightlight.toggle":
		var p struct{}
		if err := decode(&p); err != nil {
			return nil, err
		}
		r.nightLight.Toggle()
		state := r.nightLight.State()
		return map[string]any{"on": state.Target > 0, "override": state.Override}, nil
	case "nightlight.temperature":
		var p struct {
			Kelvin *int `json:"kelvin"`
		}
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Kelvin == nil {
			return nil, errors.New("nightlight.temperature needs kelvin")
		}
		if _, err := r.updateConfig(func(cfg *config.Config, registryFor func(config.Config) *settings.Registry) error {
			return themeEntrySet(registryFor(*cfg), cfg, "night-light.night-kelvin", strconv.Itoa(*p.Kelvin))
		}); err != nil {
			return nil, err
		}
		return map[string]any{"kelvin": *p.Kelvin}, nil
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}

// UpdateNightLight rebuilds the open views from the latest service snapshot.
// The caller does not pass the snapshot so a queued stale update cannot
// overwrite a newer timer or gamma event.
func (r *Registry) UpdateNightLight() {
	r.mu.Lock()
	controlOut, controlOpen := r.rebuildControlCentreLocked()
	settings := r.panelHosts[PanelSettings]
	settingsOpen := settings != nil && settings.section == "Night Light"
	if settingsOpen {
		r.rebuildPanel(settings)
	}
	settingsOut := uint32(0)
	if settingsOpen {
		settingsOut = settings.output
	}
	r.mu.Unlock()
	if controlOpen {
		r.publishSurface(controlOut, panelSurfaceID(PanelControlCenter))
	}
	if settingsOpen {
		r.publishSurface(settingsOut, panelSurfaceID(PanelSettings))
	}
}

func nightLightStatusText(state services.NightLightState) string {
	if !state.Available {
		if state.Reason != "" {
			return state.Reason
		}
		return "Waiting for gamma control."
	}
	if state.Kelvin == 0 && state.Reason != "" {
		return state.Reason
	}
	parts := []string{}
	if state.Kelvin == 0 {
		parts = append(parts, "Off")
	} else {
		parts = append(parts, "Current: "+strconv.Itoa(state.Kelvin)+" K")
	}
	if !state.NextChange.IsZero() {
		parts = append(parts, "next change "+state.NextChange.Format("15:04"))
	}
	if state.Source != "" {
		parts = append(parts, "Location: "+state.Source)
	}
	if state.Reason != "" {
		parts = append(parts, state.Reason)
	}
	return strings.Join(parts, " · ")
}

func nightLightTooltip(state services.NightLightState) string {
	if !state.Available || state.Kelvin == 0 && state.Reason != "" {
		if state.Reason != "" {
			return state.Reason
		}
		return "Waiting for gamma control."
	}
	value := "Off"
	if state.Target > 0 {
		value = strconv.Itoa(state.Target) + " K"
	}
	if !state.NextChange.IsZero() {
		value += " until " + state.NextChange.Format("15:04")
	} else if state.Override {
		value += " until toggled"
	}
	if state.Reason != "" {
		value += " · " + state.Reason
	}
	return value
}
