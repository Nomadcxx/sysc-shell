package shell

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
)

// onbMachine is what this computer has that the default set depends on.
type onbMachine struct {
	battery, wifi, thinkpad bool
	// has reports whether a command is on PATH.
	has func(string) bool
}

// onbSource is the catalog the default set installs from: the first-party one.
const onbSource = "sysc"

// onbDefaultItems are native bar items the Plugins step offers on a bar that
// lacks them. config.Default() already carries the rest of the base bar --
// the wordmark with time and date, the system monitor, clipboard, sound and
// notifications -- and Region places weather once a location is set.
var onbDefaultItems = []struct {
	id   string
	want func(onbMachine) bool
}{
	{"volume", func(onbMachine) bool { return true }},
	{"battery", func(m onbMachine) bool { return m.battery }},
	{"wifi", func(m onbMachine) bool { return m.wifi }},
}

// onbCorePlugins are recommended to everyone (owner, 2026-11-01).
var onbCorePlugins = []string{"org.sysc.calendar", "org.sysc.timer"}

// onbGenericCommands are on nearly every install, so a plugin requiring one
// says nothing about what this user runs: it stays in the store.
var onbGenericCommands = []string{"pacman", "python3"}

// onbRecGroup is where a recommendation is listed on the Plugins step.
type onbRecGroup int

const (
	onbGroupBuiltIn onbRecGroup = iota
	onbGroupCore
	onbGroupFound
)

// onbRec is one recommendation on the Plugins step: a native item to place or
// a catalog plugin to install and place. Every recommendation starts checked.
type onbRec struct {
	// key is the selection key: "item:<id>" for a native item, else the
	// plugin ID.
	key     string
	item    string
	listing store.Listing
	group   onbRecGroup
}

func (rec onbRec) native() bool { return rec.item != "" }

// onbFound reports whether a catalog plugin belongs to something this
// computer already has: a tool it requires that is installed, or ThinkPad
// hardware. The catalog's required commands are the signal, so a new plugin
// joins without a list to maintain here.
func onbFound(m onbMachine, l store.Listing) bool {
	if l.Entry.ID == "org.sysc.thinkpad" {
		return m.thinkpad
	}
	release := l.Resolution.Release
	if release == nil {
		release = &l.Entry.Release
	}
	commands := release.Requires.Commands
	if len(commands) == 0 || m.has == nil {
		return false
	}
	for _, cmd := range commands {
		if slices.Contains(onbGenericCommands, cmd) || !m.has(cmd) {
			return false
		}
	}
	return true
}

// onbRecommendations lists what the default set would add to this computer.
// Only what is not there yet is recommended: a native item already on a bar,
// and a plugin already installed or queued, are left out, so the step never
// offers what the user already has.
func onbRecommendations(m onbMachine, cfg config.Config, listings []store.Listing, installed []plugin.Candidate, queued map[string]onbInstall) []onbRec {
	var out []onbRec
	for _, d := range onbDefaultItems {
		if d.want(m) && !onbConfigHasItem(cfg, d.id) {
			out = append(out, onbRec{key: "item:" + d.id, item: d.id, group: onbGroupBuiltIn})
		}
	}
	offerable := func(l store.Listing) bool {
		if l.Source != onbSource || l.Installed != nil || l.Status != store.StatusAvailable {
			return false
		}
		if slices.ContainsFunc(installed, func(c plugin.Candidate) bool { return c.Manifest.ID == l.Entry.ID }) {
			return false
		}
		q, ok := queued[l.Entry.ID]
		return !ok || q.failed
	}
	for _, id := range onbCorePlugins {
		i := slices.IndexFunc(listings, func(l store.Listing) bool { return l.Source == onbSource && l.Entry.ID == id })
		if i >= 0 && offerable(listings[i]) {
			out = append(out, onbRec{key: id, listing: listings[i], group: onbGroupCore})
		}
	}
	for _, l := range listings {
		if !slices.Contains(onbCorePlugins, l.Entry.ID) && offerable(l) && onbFound(m, l) {
			out = append(out, onbRec{key: l.Entry.ID, listing: l, group: onbGroupFound})
		}
	}
	return out
}

// onbRecSelected is a recommendation's staged selection, or its default.
func onbRecSelected(rec onbRec, selections map[string]bool) bool {
	if selected, ok := selections[rec.key]; ok {
		return selected
	}
	return true
}

func onbConfigHasItem(cfg config.Config, id string) bool {
	match := func(item config.Item) bool { return item.ID == id }
	if onbBarContains(cfg.Bar, match) {
		return true
	}
	for _, output := range cfg.Outputs {
		if onbBarContains(output.Bar, match) {
			return true
		}
	}
	return false
}

// onbRightInsertIndex is where the wizard adds to a right lane: before the
// notifications bell, which ends the default bar, or at the end without one.
func onbRightInsertIndex(items []config.Item) int {
	if i := slices.IndexFunc(items, func(it config.Item) bool { return it.ID == "notifications" }); i >= 0 {
		return i
	}
	return len(items)
}

// onbEnsureItemPlacement adds a native item to every bar's right lane that
// lacks it, where plugin placements go.
func onbEnsureItemPlacement(cfg *config.Config, id string) error {
	ensure := func(bar *config.Bar) error {
		if onbBarContains(*bar, func(item config.Item) bool { return item.ID == id }) {
			return nil
		}
		items, err := config.InsertItem(bar.Right, onbRightInsertIndex(bar.Right), config.Item{ID: id})
		if err != nil {
			return err
		}
		bar.Right = items
		return nil
	}
	if err := ensure(&cfg.Bar); err != nil {
		return err
	}
	for i := range cfg.Outputs {
		if err := ensure(&cfg.Outputs[i].Bar); err != nil {
			return err
		}
	}
	return nil
}

// probeOnboardingHardware reads the hardware the default set depends on.
// sysc-metrics reports a battery, and the thinkpad_acpi fan interface marks a
// ThinkPad; it has no wireless flag, so that is read from the kernel's
// interface list.
func probeOnboardingHardware() onbMachine {
	var m onbMachine
	if b, err := metrics.ReadBattery(); err == nil {
		m.battery = b.Present
	}
	if f, err := metrics.ReadFan(); err == nil {
		m.thinkpad = f.Present
	}
	m.wifi = hasWirelessInterface("/sys/class/net")
	return m
}

// hasWirelessInterface reports whether any interface under root is wireless:
// the kernel gives each one a "wireless" directory.
func hasWirelessInterface(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), "wireless")); err == nil {
			return true
		}
	}
	return false
}

// onbMachineFor detects once per wizard: the answer does not change while it
// is open, and a battery read on every rebuild is wasted work.
func (r *Registry) onbMachineFor(h *PanelHost) onbMachine {
	if h.onbMachine != nil {
		return *h.onbMachine
	}
	m := onbMachine{}
	if r.onboardingHardware != nil {
		m = r.onboardingHardware()
	}
	if lookPath := r.lookPath; lookPath != nil {
		found := map[string]bool{}
		m.has = func(cmd string) bool {
			ok, seen := found[cmd]
			if !seen {
				_, err := lookPath(cmd)
				ok = err == nil
				found[cmd] = ok
			}
			return ok
		}
	}
	h.onbMachine = &m
	return m
}

func (r *Registry) onbRecommendationsFor(h *PanelHost) []onbRec {
	return onbRecommendations(r.onbMachineFor(h), h.draft, r.pluginStoreSnapshot.Listings, onbPluginCandidates(r), r.onboardingInstalls)
}

// onbCatalogState is whether the first-party catalog can be offered.
type onbCatalogState int

const (
	onbCatalogReady onbCatalogState = iota
	onbCatalogLoading
	onbCatalogUnavailable
)

func onbCatalogStatus(r *Registry) onbCatalogState {
	if r.pluginStore == nil {
		return onbCatalogUnavailable
	}
	snapshot := r.pluginStoreSnapshot
	if slices.ContainsFunc(snapshot.Listings, func(l store.Listing) bool { return l.Source == onbSource }) {
		return onbCatalogReady
	}
	for _, s := range snapshot.Sources {
		if s.Name == onbSource && s.Err != nil {
			return onbCatalogUnavailable
		}
	}
	if r.onboardingCatalogErr != nil {
		return onbCatalogUnavailable
	}
	return onbCatalogLoading
}

// requestOnboardingCatalogLocked fetches the first-party catalog once when
// the wizard opens without it, so the Plugins step has something to offer on
// a fresh install. The store keeps no catalog until something refreshes it.
func (r *Registry) requestOnboardingCatalogLocked() {
	if r.pluginStore == nil || r.onboardingCatalogRequested {
		return
	}
	if slices.ContainsFunc(r.pluginStoreSnapshot.Listings, func(l store.Listing) bool { return l.Source == onbSource }) {
		return
	}
	r.onboardingCatalogRequested = true
	if _, err := r.pluginStore.Refresh(); err != nil {
		r.onboardingCatalogErr = err
		log.Printf("shell: onboarding plugin catalog: %v", err)
	}
}

// onbInstall is one install the wizard queued. The store reports the outcome
// only through its snapshot, so prevErr tells a new failure from the error
// the listing already carried.
type onbInstall struct {
	name    string
	prevErr error
	failed  bool
}

type onbInstallState int

const (
	onbInstalling onbInstallState = iota
	onbInstalled
	onbInstallFailed
)

func onbInstallStatus(q onbInstall, listings []store.Listing, id string) (onbInstallState, error) {
	i := slices.IndexFunc(listings, func(l store.Listing) bool { return l.Source == onbSource && l.Entry.ID == id })
	if i < 0 {
		return onbInstalling, nil
	}
	l := listings[i]
	switch {
	case l.Installed != nil:
		return onbInstalled, nil
	case l.Err != nil && l.Err != q.prevErr:
		return onbInstallFailed, l.Err
	}
	return onbInstalling, nil
}

// syncOnboardingInstallsLocked marks queued installs that failed, so the
// Plugins step offers them again, and reports whether any is still running.
func (r *Registry) syncOnboardingInstallsLocked() (running bool) {
	for id, q := range r.onboardingInstalls {
		state, _ := onbInstallStatus(q, r.pluginStoreSnapshot.Listings, id)
		switch state {
		case onbInstalling:
			running = true
		case onbInstallFailed:
			q.failed = true
			r.onboardingInstalls[id] = q
		}
	}
	return running
}

// onbPendingInstalls are the checked catalog recommendations: what "Install
// and continue" would install.
func (r *Registry) onbPendingInstalls(h *PanelHost) []onbRec {
	var out []onbRec
	for _, rec := range r.onbRecommendationsFor(h) {
		if !rec.native() && onbRecSelected(rec, h.onbPluginSelections) {
			out = append(out, rec)
		}
	}
	return out
}

// queueOnboardingInstallsLocked installs every checked catalog
// recommendation at the release the consent block showed. Each is staged as
// selected, so Finish places it once the plugin host has discovered it.
func (r *Registry) queueOnboardingInstallsLocked(h *PanelHost) error {
	pending := r.onbPendingInstalls(h)
	if len(pending) == 0 {
		return nil
	}
	if r.pluginStore == nil {
		return errors.New("plugin store unavailable")
	}
	locale := r.pluginStoreDetailLocale(h)
	if r.onboardingInstalls == nil {
		r.onboardingInstalls = map[string]onbInstall{}
	}
	if h.onbPluginSelections == nil {
		h.onbPluginSelections = map[string]bool{}
	}
	for _, rec := range pending {
		detail := detailFor(rec.listing, false, nil, "", locale)
		if _, err := r.pluginStore.Install(rec.listing.Source, rec.listing.Entry.ID, detail.ReleaseRef); err != nil {
			return fmt.Errorf("%s: %w", detail.Title, err)
		}
		r.onboardingInstalls[rec.key] = onbInstall{name: detail.Title, prevErr: rec.listing.Err}
		h.onbPluginSelections[rec.key] = true
	}
	return nil
}

// applyOnboardingItems places the checked native recommendations.
func (r *Registry) applyOnboardingItems(h *PanelHost, cfg *config.Config) error {
	for _, rec := range r.onbRecommendationsFor(h) {
		if rec.native() && onbRecSelected(rec, h.onbPluginSelections) {
			if err := onbEnsureItemPlacement(cfg, rec.item); err != nil {
				return err
			}
		}
	}
	return nil
}
