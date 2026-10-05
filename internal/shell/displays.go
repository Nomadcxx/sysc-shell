package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/displays"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// displaysController is the one service surface the page uses. Every command
// blocks on the compositor, on a file and on systemd, so none of them may be
// called from a configure, render or handle path: the page starts them from a
// goroutine and reads the immutable snapshot they publish in return.
type displaysController interface {
	Subscribe() <-chan displays.Snapshot
	Refresh()
	Preview(ctx context.Context, draft map[string]displays.Policy) error
	Keep(ctx context.Context) error
	Revert(ctx context.Context) error
	Close(ctx context.Context) error
	Root() string
}

// displaysCommandTimeout caps one page command. The armed window is twenty
// seconds and the apply budget five; a command that has not answered by now is
// finished either way, and the independent recovery unit still owns the
// outcome.
const displaysCommandTimeout = 30 * time.Second

// SetDisplaysService attaches the one service that owns display settings. It is
// only present once the user has confirmed a configuration root, so a shell
// whose config records no root has nothing to attach and the page says so.
func (r *Registry) SetDisplaysService(svc displaysController) {
	if r == nil || svc == nil {
		return
	}
	r.mu.Lock()
	if r.displaysService != nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-r.closed:
		r.mu.Unlock()
		return
	default:
	}
	r.displaysService = svc
	if root := svc.Root(); root != "" {
		r.displaysRoot = root
	}
	r.mu.Unlock()
	go r.relayDisplays(svc)
}

func (r *Registry) relayDisplays(svc displaysController) {
	updates := svc.Subscribe()
	for {
		select {
		case <-r.closed:
			return
		case snapshot, ok := <-updates:
			if !ok {
				return
			}
			r.applyDisplaysSnapshot(svc, snapshot)
		}
	}
}

func (r *Registry) applyDisplaysSnapshot(svc displaysController, snapshot displays.Snapshot) {
	r.mu.Lock()
	if r.displaysService != svc {
		r.mu.Unlock()
		return
	}
	// A preview that has just ended settles the draft: the transaction is over,
	// so what the outputs report now is the truth the page should show again.
	if r.displaysArmed && !snapshot.Preview {
		r.displaysDraft = nil
	}
	r.displaysArmed = snapshot.Preview
	r.displaysSnapshot = snapshot
	syncDisplaysDraftLocked(r, snapshot)
	var publish []waylandSurface
	if h := r.panelHosts[PanelSettings]; h != nil && h.section == displaysSection {
		r.rebuildPanel(h)
		publish = append(publish, waylandSurface{output: h.output, id: panelSurfaceID(h.id)})
	}
	r.mu.Unlock()
	for _, surface := range publish {
		r.publishSurface(surface.output, surface.id)
	}
}

// displaysCommandLocked starts one page command off the Wayland owner and off
// the registry lock. Caller holds Registry.mu.
func (r *Registry) displaysCommandLocked(h *PanelHost, what string, run func(context.Context) error) {
	if h == nil || r.displaysBusy {
		return
	}
	r.displaysBusy = true
	r.displaysErr = ""
	r.rebuildPanel(h)
	surface := waylandSurface{output: h.output, id: panelSurfaceID(h.id)}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), displaysCommandTimeout)
		defer cancel()
		err := run(ctx)
		r.mu.Lock()
		if current := r.panelHosts[PanelSettings]; current != nil {
			h = current
		}
		r.displaysBusy = false
		if err != nil {
			r.displaysErr = err.Error()
		} else if r.displaysService != nil {
			r.displaysService.Refresh()
		}
		if h != nil {
			r.rebuildPanel(h)
		}
		r.mu.Unlock()
		if err != nil {
			log.Printf("shell: displays %s: %v", what, err)
		}
		r.publishSurface(surface.output, surface.id)
	}()
}

// displaysPreviewLocked arms a preview of the draft. Caller holds Registry.mu.
func (r *Registry) displaysPreviewLocked(h *PanelHost) {
	svc := r.displaysService
	if svc == nil {
		r.displaysRefuseLocked(h, displaysNotSetUpNote)
		return
	}
	target, err := displaysTarget(r.displaysDraft)
	if err != nil {
		r.displaysRefuseLocked(h, err.Error())
		return
	}
	draft := target
	r.displaysCommandLocked(h, "preview", func(ctx context.Context) error { return svc.Preview(ctx, draft) })
}

// displaysKeepLocked makes the preview durable. Caller holds Registry.mu.
func (r *Registry) displaysKeepLocked(h *PanelHost) {
	svc := r.displaysService
	switch {
	case svc == nil:
		r.displaysRefuseLocked(h, displaysNotSetUpNote)
	case !r.displaysArmed:
		r.displaysRefuseLocked(h, "Nothing is previewed, so there is nothing to keep. Preview a change first.")
	default:
		r.displaysCommandLocked(h, "keep", svc.Keep)
	}
}

// displaysRevertLocked puts the pre-preview state back. It is the answer to
// Escape, to leaving the page and to closing the panel, so it is idempotent and
// silent when nothing is armed. Caller holds Registry.mu.
func (r *Registry) displaysRevertLocked(h *PanelHost) {
	svc := r.displaysService
	if svc == nil || !r.displaysArmed {
		return
	}
	r.displaysCommandLocked(h, "revert", svc.Revert)
}

// displaysRefuseLocked says why a control is unavailable, in the page's own
// error line. Caller holds Registry.mu.
func (r *Registry) displaysRefuseLocked(h *PanelHost, why string) {
	r.displaysErr = why
	if h != nil {
		r.rebuildPanel(h)
	}
}

// displaysAdoptLocked discovers the compositor's configuration root and, only
// when the shell is prepared to write there, adopts it. Caller holds
// Registry.mu.
func (r *Registry) displaysAdoptLocked(h *PanelHost) {
	if r.displaysBusy || r.displaysService != nil {
		return
	}
	socket := os.Getenv("NIRI_SOCKET")
	if socket == "" {
		r.displaysRefuseLocked(h, "NIRI_SOCKET is unset, so the compositor's configuration cannot be found.")
		return
	}
	r.displaysCommandLocked(h, "adopt", func(ctx context.Context) error {
		return r.adoptDisplays(ctx, socket)
	})
}

// adoptDisplays runs off the Wayland owner: it reads the compositor's process
// for its root, edits that root, and constructs the service. The root it lands
// on was shown on the page and confirmed by the user before this was called.
func (r *Registry) adoptDisplays(ctx context.Context, socket string) error {
	found, err := displays.Discover(socket)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.displaysDiscovery = &found
	r.mu.Unlock()
	if found.ReadOnly {
		return errors.New(found.Note)
	}
	if _, err := displays.Setup(found.Root); err != nil {
		return err
	}
	validate, err := displays.NiriValidator()
	if err != nil {
		// The root is adopted either way: the page will say that publishing is
		// unavailable until a native niri can validate a candidate.
		log.Printf("shell: displays: %v", err)
	}
	svc, err := displays.New(socket, found.Root, validate)
	if err != nil {
		return err
	}
	// The root is remembered so the next start needs no second confirmation.
	r.mu.Lock()
	next := r.cfg
	next.Displays.NiriConfigPath = found.Root
	err = r.writeConfig(next)
	if err == nil {
		r.cfg = next
	}
	r.displaysRoot = found.Root
	r.displaysErr = ""
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.SetDisplaysService(svc)
	return nil
}

// displaysRefreshLocked asks for a fresh reading of the outputs and of the
// compositor's configuration root. Caller holds Registry.mu.
func (r *Registry) displaysRefreshLocked(h *PanelHost) {
	if r.displaysBusy {
		return
	}
	if r.displaysService != nil {
		r.displaysService.Refresh()
	}
	if socket := os.Getenv("NIRI_SOCKET"); socket != "" {
		r.displaysCommandLocked(h, "discover", func(context.Context) error {
			found, err := displays.Discover(socket)
			if err != nil {
				return err
			}
			r.mu.Lock()
			r.displaysDiscovery = &found
			r.mu.Unlock()
			return nil
		})
		return
	}
	if h != nil {
		r.rebuildPanel(h)
	}
}

// syncDisplaysDraftLocked fills in the outputs the draft has never mentioned
// from the snapshot. A value the user has typed is never overwritten, and a
// preview is what the page shows either way.
func syncDisplaysDraftLocked(r *Registry, snapshot displays.Snapshot) {
	if r.displaysDraft == nil {
		r.displaysDraft = make(map[string]map[string]string, len(snapshot.Outputs))
	}
	for _, out := range snapshot.Outputs {
		fields := r.displaysDraft[out.Name.Connector]
		if fields == nil {
			fields = map[string]string{}
			r.displaysDraft[out.Name.Connector] = fields
		}
		seedDisplaysDraft(fields, out.Policy)
	}
}

func seedDisplaysDraft(fields map[string]string, p displays.Policy) {
	if _, ok := fields[displaysFieldMode]; !ok {
		fields[displaysFieldMode] = displaysModeValue(p.Mode)
	}
	if _, ok := fields[displaysFieldScale]; !ok {
		fields[displaysFieldScale] = displaysScaleValue(p.Scale)
	}
	if _, ok := fields[displaysFieldTransform]; !ok {
		fields[displaysFieldTransform] = displaysTransformValue(p.Transform)
	}
	if _, ok := fields[displaysFieldPositionX]; !ok {
		fields[displaysFieldPositionX] = displaysPositionValue(p.Position)
	}
	if _, ok := fields[displaysFieldPositionY]; !ok {
		fields[displaysFieldPositionY] = displaysPositionValueY(p.Position)
	}
	if _, ok := fields[displaysFieldVRR]; !ok {
		fields[displaysFieldVRR] = displaysVRRValue(p.VRR)
	}
}

// setDisplaysDraftLocked records one edited field. Nothing is sent anywhere: a
// draft is a draft until Preview arms a transaction. Caller holds Registry.mu.
func (r *Registry) setDisplaysDraftLocked(h *PanelHost, key, value string) {
	connector, field, ok := strings.Cut(key, ".")
	if !ok || connector == "" || field == "" {
		return
	}
	if r.displaysDraft == nil {
		r.displaysDraft = make(map[string]map[string]string)
	}
	fields := r.displaysDraft[connector]
	if fields == nil {
		fields = map[string]string{}
		r.displaysDraft[connector] = fields
	}
	fields[field] = value
	if h != nil {
		r.rebuildPanel(h)
	}
}

const (
	displaysFieldMode       = "mode"
	displaysFieldScale      = "scale"
	displaysFieldTransform  = "transform"
	displaysFieldPositionX  = "position.x"
	displaysFieldPositionY  = "position.y"
	displaysFieldVRR        = "vrr"
	displaysAuto            = "auto"
	displaysVRROnDemand     = "on-demand"
	displaysSection         = "Display"
	displaysNotSetUpNote    = "Display settings are not set up. Adopt the compositor's configuration to enable them."
	displaysNoValidatorNote = "niri validate is not available, so a candidate cannot be checked before it is published. Nothing will be changed."
)

// displaysTarget turns the edited draft into the policies a transaction
// applies. A field the page could not read as a value is reported with its
// name rather than silently dropped.
func displaysTarget(draft map[string]map[string]string) (map[string]displays.Policy, error) {
	target := make(map[string]displays.Policy, len(draft))
	for _, connector := range slices.Sorted(maps.Keys(draft)) {
		fields := draft[connector]
		var p displays.Policy
		if v, ok := fields[displaysFieldMode]; ok {
			mode, err := displaysModeFrom(v)
			if err != nil {
				return nil, fmt.Errorf("%s resolution: %v", connector, err)
			}
			p.Mode = mode
		}
		if v, ok := fields[displaysFieldScale]; ok {
			scale, err := displaysScaleFrom(v)
			if err != nil {
				return nil, fmt.Errorf("%s scale: %v", connector, err)
			}
			p.Scale = scale
		}
		if v, ok := fields[displaysFieldTransform]; ok {
			if v != displaysAuto {
				if err := displays.ParseTransformNative(v); err != nil {
					return nil, fmt.Errorf("%s transform: %v", connector, err)
				}
				name := displays.TransformPolicy(v)
				p.Transform = &name
			}
		}
		if _, ok := fields[displaysFieldPositionX]; ok {
			pos, err := displaysPositionFrom(fields)
			if err != nil {
				return nil, fmt.Errorf("%s position: %v", connector, err)
			}
			p.Position = pos
		}
		if v, ok := fields[displaysFieldVRR]; ok {
			vrr, err := displaysVRRFrom(v)
			if err != nil {
				return nil, fmt.Errorf("%s refresh rate: %v", connector, err)
			}
			p.VRR = vrr
		}
		target[connector] = p
	}
	return target, nil
}

func displaysModeValue(m *displays.ModePolicy) string {
	if m == nil {
		return displaysAuto
	}
	return fmt.Sprintf("%dx%d@%d", m.Width, m.Height, m.RefreshMilliHz)
}

func displaysModeLabel(m displays.Mode) string {
	return fmt.Sprintf("%d × %d @ %s", m.Width, m.Height, displaysRefreshLabel(m.RefreshMilliHz))
}

func displaysRefreshLabel(milli uint32) string {
	return fmt.Sprintf("%.3f Hz", float64(milli)/1000)
}

func displaysModeFrom(v string) (*displays.ModePolicy, error) {
	if v == displaysAuto || v == "" {
		return nil, nil
	}
	size, refresh, ok := strings.Cut(v, "@")
	if !ok {
		return nil, fmt.Errorf("%q is not a resolution and refresh rate", v)
	}
	w, h, ok := strings.Cut(size, "x")
	if !ok {
		return nil, fmt.Errorf("%q is not WIDTHxHEIGHT@REFRESH", v)
	}
	width, err := strconv.ParseUint(w, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("%q has no width", v)
	}
	height, err := strconv.ParseUint(h, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("%q has no height", v)
	}
	milli, err := strconv.ParseUint(refresh, 10, 32)
	if err != nil || milli == 0 {
		return nil, fmt.Errorf("%q has no refresh rate in millihertz", v)
	}
	return &displays.ModePolicy{Width: uint16(width), Height: uint16(height), RefreshMilliHz: uint32(milli)}, nil
}

func displaysScaleValue(scale *float64) string {
	if scale == nil {
		return displaysAuto
	}
	return strconv.FormatFloat(*scale, 'f', -1, 64)
}

func displaysScaleFrom(v string) (*float64, error) {
	if v == displaysAuto || v == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number", v)
	}
	if err := displays.ValidateScale(f); err != nil {
		return nil, err
	}
	return &f, nil
}

func displaysTransformValue(t *displays.TransformPolicy) string {
	if t == nil {
		return displaysAuto
	}
	return string(*t)
}

func displaysPositionValue(p *displays.Position) string {
	if p == nil {
		return displaysAuto
	}
	return strconv.Itoa(int(p.X))
}

func displaysPositionValueY(p *displays.Position) string {
	if p == nil {
		return displaysAuto
	}
	return strconv.Itoa(int(p.Y))
}

// displaysPositionFrom reads both halves, so a half-typed pair fails with a
// message about the position rather than a stale coordinate.
func displaysPositionFrom(fields map[string]string) (*displays.Position, error) {
	x, xok := fields[displaysFieldPositionX]
	y, yok := fields[displaysFieldPositionY]
	if (x == displaysAuto || x == "") && (y == displaysAuto || y == "") {
		return nil, nil
	}
	if !xok || !yok || x == displaysAuto || y == displaysAuto || x == "" || y == "" {
		return nil, errors.New("both X and Y are needed, or Automatic for both")
	}
	xv, err := strconv.ParseInt(x, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("X %q is not a whole number of logical pixels", x)
	}
	yv, err := strconv.ParseInt(y, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("Y %q is not a whole number of logical pixels", y)
	}
	return &displays.Position{X: int32(xv), Y: int32(yv)}, nil
}

func displaysVRRValue(vrr *displays.VRRPolicy) string {
	switch {
	case vrr == nil:
		return "false"
	case vrr.OnDemand:
		return displaysVRROnDemand
	default:
		return "true"
	}
}

func displaysVRRFrom(v string) (*displays.VRRPolicy, error) {
	switch v {
	case "", "false", "true":
		// The two are the same policy: niri's absent node and a node without
		// on-demand both mean "vary the refresh as it needs to".
		return &displays.VRRPolicy{}, nil
	case displaysVRROnDemand:
		return &displays.VRRPolicy{OnDemand: true}, nil
	default:
		return nil, fmt.Errorf("%q is not off, on or on-demand", v)
	}
}

// focusDisplaysPreview puts the keyboard on Revert whenever a display preview
// is armed, unless the user has already moved somewhere deliberate. Enter
// activates the focused control, so the answer that undoes the change is the one
// the keyboard starts on and keeping is never a single keypress by accident.
func (h *PanelHost) focusDisplaysPreview() {
	const revert, keep = "Revert preview", "Keep preview"
	for i, n := range h.focus {
		if n == nil || n.Name != revert {
			continue
		}
		if cur := h.focused(); cur != nil {
			// A half-typed coordinate and a deliberate move to Keep are the
			// keyboard being used, so neither is taken back. Everything else,
			// including where the panel was when the preview started, yields to
			// the answer that undoes the change.
			if (cur.Kind == ui.KindTextField && cur.Name != "Search") || cur.Name == keep {
				return
			}
		}
		h.roving.Set(i)
		return
	}
}
