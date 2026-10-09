package shell

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// assetNow is the clock every asset shows: a weekday late morning.
var assetNow = time.Date(2026, 10, 9, 11, 42, 0, 0, time.Local)

// assetSample is a workstation under light use, the readings the monitor and
// the control centre draw.
func assetSample() services.Snapshot {
	return services.Snapshot{
		CollectedAt: assetNow,
		CPU:         &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.27, Valid: true}},
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 32 << 30, UsedBytes: 11 << 30},
		},
		Thermal:   &metrics.ThermalSnapshot{Celsius: 54, Valid: true},
		GPU:       &metrics.GPUSnapshot{GPUs: []metrics.GPU{{Usage: metrics.GPUUsage{Fraction: 0.12, Valid: true}}}},
		Processes: assetProcesses(),
		Battery: &metrics.BatterySnapshot{Present: true, Charge: 0.82, ChargeValid: true, State: metrics.BatteryDischarging,
			TimeRemaining: 4*time.Hour + 20*time.Minute, TimeValid: true},
	}
}

// assetMedia is a track playing in an invented player.
var assetMedia = services.MediaState{
	Available: true, Identity: "Player", Title: "Northern Lights", Artist: "The Lanterns", Album: "Long Exposure",
	Status: services.PlaybackPlaying, PositionUS: 94e6, LengthUS: 231e6, Rate: 1,
	CanNext: true, CanPrev: true, CanPlay: true, CanPause: true, CanSeek: true,
}

// stubWpctl gives the registry an audio service that reads a fixed volume from
// a stand-in wpctl, so a screenshot never shows the machine it was taken on.
func stubWpctl(t *testing.T, reg *Registry, volume string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wpctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'Volume: "+volume+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	audio := services.NewAudio(time.Hour, path)
	t.Cleanup(audio.Close)
	lease, err := audio.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := audio.CachedState(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in wpctl was never polled")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reg.audio = audio
}

// assetWeather is a mild autumn morning in a place that does not exist.
func assetWeather() services.Reading {
	day := true
	humidity, wind := 61.0, 14.0
	feels := 15.0
	return services.Reading{
		Observed: true, Temperature: 17, Unit: services.UnitCelsius, Code: 2,
		Apparent: &feels, IsDay: &day, Humidity: &humidity, WindSpeed: &wind,
		Location: "Northgate", FetchedAt: assetNow, Daily: assetForecast(),
	}
}

// assetMachine is the hardware card: an invented workstation, never the one
// the screenshot was taken on.
var assetMachine = machineFacts{
	Distro: "Arch Linux", Kernel: "6.17.4-arch1-1", CPU: "Quantum Q7 8-Core Processor",
	Board: "Acme Systems A550M", Uptime: "3 hours", UptimeLong: "0 days 3 hours 12 minutes",
	Logo: "archlinux", LogoLetter: "A",
}

// assetProcesses is a plausible process table of invented programs.
func assetProcesses() *metrics.ProcessSnapshot {
	proc := func(pid int, name string, cpu float64, mib uint64) metrics.Process {
		return metrics.Process{
			Identity: metrics.ProcessIdentity{PID: pid, StartTimeTicks: uint64(pid)}, Name: name,
			UID: 1000, UIDValid: true, ResidentBytes: mib << 20, ResidentValid: true,
			CPU: metrics.CPUUsage{Fraction: cpu, Valid: true},
		}
	}
	return &metrics.ProcessSnapshot{CollectedAt: assetNow, Processes: []metrics.Process{
		proc(2201, "browser", 0.09, 1480), proc(2310, "editor", 0.05, 820), proc(1980, "compositor", 0.04, 310),
		proc(2455, "terminal", 0.03, 190), proc(2602, "music-player", 0.02, 240), proc(1744, "sysc-shell", 0.01, 120),
		proc(2788, "chat", 0.01, 560), proc(2891, "build-daemon", 0.01, 410),
	}}
}

// assetForecast is five days of invented weather.
func assetForecast() []services.Day {
	day := func(date string, code int, high, low float64) services.Day {
		return services.Day{Date: date, Code: code, High: high, Low: low, Sunrise: date + "T06:42", Sunset: date + "T18:51"}
	}
	return []services.Day{
		day("2026-10-09", 2, 18, 9), day("2026-10-10", 3, 16, 10), day("2026-10-11", 61, 14, 9),
		day("2026-10-12", 1, 17, 8), day("2026-10-13", 0, 19, 8),
	}
}

// assetCalendarState is the calendar plugin's published snapshot: a dozen
// invented events across two calendars.
const assetCalendarState = `{"control_center":{"generated":"2026-10-09T11:00:00Z","sources":2,` +
	`"days":{"2026-10-09":3,"2026-10-12":1,"2026-10-14":2,"2026-10-17":1,"2026-10-24":1,"2026-10-28":1},"upcoming":[` +
	`{"start":"2026-10-09T09:00:00Z","summary":"Design review","calendar":"Work"},` +
	`{"start":"2026-10-09T12:00:00Z","summary":"Team lunch","calendar":"Work"},` +
	`{"start":"2026-10-09T17:00:00Z","summary":"Piano lesson","calendar":"Home"},` +
	`{"start":"2026-10-12T14:00:00Z","summary":"Release freeze","calendar":"Work"},` +
	`{"start":"2026-10-17T09:00:00Z","summary":"Farmers market","calendar":"Home"}]}}`

// assetGradientFile writes a w by h PNG fading diagonally between two colours
// into the test's temp directory and returns its path.
func assetGradientFile(t *testing.T, w, h int, from, to color.NRGBA) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	span := max(w+h-2, 1)
	mix := func(a, b uint8, step int) uint8 { return uint8((int(a)*(span-step) + int(b)*step) / span) }
	for y := range h {
		for x := range w {
			s := x + y
			img.SetNRGBA(x, y, color.NRGBA{mix(from.R, to.R, s), mix(from.G, to.G, s), mix(from.B, to.B, s), 255})
		}
	}
	path := filepath.Join(t.TempDir(), "gradient.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}
