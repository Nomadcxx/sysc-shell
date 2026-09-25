package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// StateStore is the namespaced persistent store a plugin may read and write.
// *Store is the production implementation; tests substitute a stalling one.
type StateStore interface {
	Get(key string) (json.RawMessage, bool)
	Keys() []string
	Set(ctx context.Context, key string, value json.RawMessage) error
}

// CallEnv is the host surface one plugin is allowed to use.
type CallEnv struct {
	PluginID          string
	Granted           []Capability
	DeclaredPanels    []Panel
	Store             StateStore
	OpenPanel         func(context.Context, v1.PanelParams) (v1.PanelResult, error)
	ClosePanel        func(context.Context, v1.PanelParams) error
	Notify            func(context.Context, v1.NotifyParams) (v1.NotifyResult, error)
	OutputContext     func(context.Context, v1.OutputContextParams) (v1.OutputContextResult, error)
	PanelResize       func(context.Context, v1.PanelResizeParams) error
	ViewFocus         func(context.Context, v1.ViewFocusParams) error
	OpenSurface       func(context.Context, v1.SurfaceOpenParams) (v1.SurfaceResult, error)
	CloseSurface      func(context.Context, v1.SurfaceCloseParams) error
	SurfacePin        func(context.Context, v1.SurfacePinParams) error
	WallpaperSnapshot func(context.Context) (v1.WallpaperSnapshotResult, error)
	WallpaperMaskSet  func(context.Context, v1.WallpaperMaskSetParams) error
	ClipboardRead     func(context.Context) (v1.ClipboardReadResult, error)
	OpenURL           func(context.Context, v1.OpenURLParams) error
	ClipboardWrite    func(context.Context, v1.ClipboardWriteParams) error
	MaxPending        int
	CallTimeout       time.Duration
}

func (e CallEnv) maxPending() int {
	if e.MaxPending > 0 {
		return e.MaxPending
	}
	return v1.DefaultLimits.PendingCalls
}

func (e CallEnv) allows(c Capability) bool {
	for _, have := range e.Granted {
		if have == c {
			return true
		}
	}
	return false
}

func (e CallEnv) hasPanel(id string) bool {
	for _, p := range e.DeclaredPanels {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Dispatcher answers host.call messages for one plugin.
//
// Capabilities are checked here, not in the store or the notification client:
// a plugin that was not granted a class of call must not reach the service,
// even if the service would have accepted it.
type Dispatcher struct {
	env     CallEnv
	mu      sync.Mutex
	pending int
}

// NewDispatcher returns a dispatcher for one plugin's host calls.
func NewDispatcher(env CallEnv) *Dispatcher {
	return &Dispatcher{env: env}
}

// Handle answers one call. It always produces a reply so the plugin can pair
// it with the id it sent; a missing service or a denied grant is an error
// reply, not a dropped line.
func (d *Dispatcher) Handle(ctx context.Context, call *v1.HostCall) v1.HostReply {
	if call == nil {
		return failReply("", "empty call")
	}
	if d.env.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.env.CallTimeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return failReply(call.ID, err.Error())
	}
	if !d.begin() {
		return failReply(call.ID, "too many pending calls")
	}
	defer d.end()

	done := make(chan v1.HostReply, 1)
	go func() { done <- d.dispatch(ctx, call) }()
	select {
	case reply := <-done:
		return reply
	case <-ctx.Done():
		return failReply(call.ID, ctx.Err().Error())
	}
}

func (d *Dispatcher) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending >= d.env.maxPending() {
		return false
	}
	d.pending++
	return true
}

func (d *Dispatcher) end() {
	d.mu.Lock()
	d.pending--
	d.mu.Unlock()
}

func (d *Dispatcher) dispatch(ctx context.Context, call *v1.HostCall) v1.HostReply {
	if err := ctx.Err(); err != nil {
		return failReply(call.ID, err.Error())
	}
	switch call.Call {
	case v1.CallStateGet, v1.CallStateSet, v1.CallStateList:
		if !d.env.allows(CapState) {
			return failReply(call.ID, "capability state is not granted")
		}
		if d.env.Store == nil {
			return failReply(call.ID, "no state store")
		}
		return d.state(ctx, call)
	case v1.CallPanelOpen, v1.CallPanelClose:
		if !d.env.allows(CapPanels) {
			return failReply(call.ID, "capability panels is not granted")
		}
		return d.panel(ctx, call)
	case v1.CallPanelResize, v1.CallViewFocus:
		if !d.env.allows(CapPanels) {
			return failReply(call.ID, "capability panels is not granted")
		}
		return d.panelSurface(ctx, call)
	case v1.CallSurfaceOpen, v1.CallSurfaceClose, v1.CallSurfacePin:
		if !d.env.allows(CapFloatingSurfaces) {
			return failReply(call.ID, "capability floating_surfaces is not granted")
		}
		return d.floatingSurface(ctx, call)
	case v1.CallNotify:
		if !d.env.allows(CapNotifications) {
			return failReply(call.ID, "capability notifications is not granted")
		}
		return d.notify(ctx, call)
	case v1.CallOutputContext:
		return d.output(ctx, call)
	case v1.CallWallpaperSnapshot, v1.CallWallpaperMaskSet:
		if !d.env.allows(CapWallpaper) {
			return failReply(call.ID, "capability wallpaper is not granted")
		}
		return d.wallpaper(ctx, call)
	case v1.CallClipboardRead:
		if !d.env.allows(CapClipboardRead) {
			return failReply(call.ID, "capability clipboard-read is not granted")
		}
		var params v1.ClipboardReadParams
		if err := decodeStrictParams(call.Params, &params); err != nil {
			return failReply(call.ID, err.Error())
		}
		if d.env.ClipboardRead == nil {
			return failReply(call.ID, "clipboard read is not available")
		}
		result, err := d.env.ClipboardRead(ctx)
		if err != nil {
			return failReply(call.ID, err.Error())
		}
		if len(result.Text) > v1.MaxInputBytes || !utf8.ValidString(result.Text) || strings.ContainsRune(result.Text, '\x00') {
			return failReply(call.ID, "clipboard returned invalid or oversized text")
		}
		return okReply(call.ID, result)
	case v1.CallOpenURL:
		if !d.env.allows(CapOpenURL) {
			return failReply(call.ID, "capability open-url is not granted")
		}
		return d.openURL(ctx, call)
	case v1.CallClipboardWrite:
		if !d.env.allows(CapClipboardWrite) {
			return failReply(call.ID, "capability clipboard-write is not granted")
		}
		return d.clipboardWrite(ctx, call)
	default:
		return failReply(call.ID, fmt.Sprintf("unknown call %q", call.Call))
	}
}

func (d *Dispatcher) floatingSurface(ctx context.Context, call *v1.HostCall) v1.HostReply {
	switch call.Call {
	case v1.CallSurfaceOpen:
		var p v1.SurfaceOpenParams
		if err := decodeStrictParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if p.Key == "" || len(p.Key) > v1.MaxIdentBytes || strings.ContainsAny(p.Key, "/\\\x00") {
			return failReply(call.ID, "surface key is empty, too long, or contains a path separator")
		}
		if len(p.Title) > v1.MaxIdentBytes {
			return failReply(call.ID, "surface title is too long")
		}
		if p.Width < 200 || p.Width > 2048 || p.Height < 120 || p.Height > 2048 {
			return failReply(call.ID, "surface size is outside 200..2048 by 120..2048")
		}
		if p.X < 0 || p.Y < 0 {
			return failReply(call.ID, "surface position cannot be negative")
		}
		if d.env.OpenSurface == nil {
			return failReply(call.ID, "floating surfaces are not available")
		}
		result, err := d.env.OpenSurface(ctx, p)
		if err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, result)
	case v1.CallSurfaceClose:
		var p v1.SurfaceCloseParams
		if err := decodeStrictParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if p.View == "" {
			return failReply(call.ID, "surface close needs a view")
		}
		if d.env.CloseSurface == nil {
			return failReply(call.ID, "floating surfaces are not available")
		}
		if err := d.env.CloseSurface(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	case v1.CallSurfacePin:
		var p v1.SurfacePinParams
		if err := decodeStrictParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if p.View == "" {
			return failReply(call.ID, "surface pin needs a view")
		}
		if d.env.SurfacePin == nil {
			return failReply(call.ID, "floating surfaces are not available")
		}
		if err := d.env.SurfacePin(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	}
	return failReply(call.ID, "unknown floating surface call")
}

func (d *Dispatcher) wallpaper(ctx context.Context, call *v1.HostCall) v1.HostReply {
	switch call.Call {
	case v1.CallWallpaperSnapshot:
		var params struct{}
		if err := decodeStrictParams(call.Params, &params); err != nil {
			return failReply(call.ID, err.Error())
		}
		if d.env.WallpaperSnapshot == nil {
			return failReply(call.ID, "wallpaper snapshot is not available")
		}
		result, err := d.env.WallpaperSnapshot(ctx)
		if err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, result)
	case v1.CallWallpaperMaskSet:
		var params v1.WallpaperMaskSetParams
		if err := decodeStrictParams(call.Params, &params); err != nil {
			return failReply(call.ID, err.Error())
		}
		if params.Output == "" {
			return failReply(call.ID, "wallpaper mask set needs an output")
		}
		if params.MaskPath != "" && params.WallpaperPath == "" {
			return failReply(call.ID, "wallpaper mask set needs a wallpaper path")
		}
		if d.env.WallpaperMaskSet == nil {
			return failReply(call.ID, "wallpaper mask set is not available")
		}
		if err := d.env.WallpaperMaskSet(ctx, params); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	}
	return failReply(call.ID, "unknown wallpaper call")
}

func (d *Dispatcher) openURL(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.OpenURLParams
	if err := decodeStrictParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if !validHTTPURL(p.URL) {
		return failReply(call.ID, "URL must be an absolute HTTP(S) address without credentials")
	}
	if d.env.OpenURL != nil {
		if err := d.env.OpenURL(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
	} else if err := openURLCommand(ctx, p.URL); err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, nil)
}

func (d *Dispatcher) clipboardWrite(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.ClipboardWriteParams
	if err := decodeStrictParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if err := validClipboardText(p.Text); err != nil {
		return failReply(call.ID, err.Error())
	}
	if d.env.ClipboardWrite != nil {
		if err := d.env.ClipboardWrite(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
	} else if err := clipboardWriteCommand(ctx, p.Text); err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, nil)
}

func validHTTPURL(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && u.Opaque == "" && u.User == nil && u.Hostname() != "" &&
		(strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

func validClipboardText(value string) error {
	if len(value) > 8192 {
		return fmt.Errorf("clipboard text exceeds 8192 bytes")
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("clipboard text is not valid UTF-8")
	}
	for _, r := range value {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return fmt.Errorf("clipboard text contains a control character")
		}
	}
	return nil
}

func openURLCommand(parent context.Context, raw string) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "xdg-open", raw).Run(); err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	return nil
}

func clipboardWriteCommand(parent context.Context, value string) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "wl-copy")
	command.Stdin = strings.NewReader(value)
	if err := command.Run(); err != nil {
		return fmt.Errorf("write clipboard: %w", err)
	}
	return nil
}

func (d *Dispatcher) state(ctx context.Context, call *v1.HostCall) v1.HostReply {
	switch call.Call {
	case v1.CallStateGet:
		var p v1.StateGetParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		value, found := d.env.Store.Get(p.Key)
		return okReply(call.ID, v1.StateGetResult{Found: found, Value: value})
	case v1.CallStateSet:
		var p v1.StateSetParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if err := d.env.Store.Set(ctx, p.Key, p.Value); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	case v1.CallStateList:
		return okReply(call.ID, v1.StateListResult{Keys: d.env.Store.Keys()})
	}
	return failReply(call.ID, "unknown state call")
}

func (d *Dispatcher) panel(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.PanelParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if p.Entry == "" || !d.env.hasPanel(p.Entry) {
		return failReply(call.ID, fmt.Sprintf("panel %q is not declared", p.Entry))
	}
	switch call.Call {
	case v1.CallPanelOpen:
		if d.env.OpenPanel == nil {
			return failReply(call.ID, "panel open is not available")
		}
		result, err := d.env.OpenPanel(ctx, p)
		if err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, result)
	case v1.CallPanelClose:
		if d.env.ClosePanel == nil {
			return failReply(call.ID, "panel close is not available")
		}
		if err := d.env.ClosePanel(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	}
	return failReply(call.ID, "unknown panel call")
}

func (d *Dispatcher) notify(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.NotifyParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if err := boundNotify(&p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if d.env.Notify == nil {
		return failReply(call.ID, "notifications are not available")
	}
	result, err := d.env.Notify(ctx, p)
	if err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, result)
}

func (d *Dispatcher) output(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.OutputContextParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if d.env.OutputContext == nil {
		return failReply(call.ID, "output context is not available")
	}
	result, err := d.env.OutputContext(ctx, p)
	if err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, result)
}

// panelSurface serves the two calls that operate on an already-open panel
// surface: resizing it and focusing one of its nodes.
func (d *Dispatcher) panelSurface(ctx context.Context, call *v1.HostCall) v1.HostReply {
	switch call.Call {
	case v1.CallPanelResize:
		var p v1.PanelResizeParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if err := boundPanelResize(&p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if d.env.PanelResize == nil {
			return failReply(call.ID, "panel resize is not available")
		}
		if err := d.env.PanelResize(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	case v1.CallViewFocus:
		var p v1.ViewFocusParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if p.View == "" || p.Node == "" {
			return failReply(call.ID, "view focus needs a view and a node")
		}
		if d.env.ViewFocus == nil {
			return failReply(call.ID, "view focus is not available")
		}
		if err := d.env.ViewFocus(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	}
	return failReply(call.ID, "unknown panel surface call")
}

const (
	minPanelExtent = 64
	maxPanelExtent = 4096
)

// boundPanelResize keeps a plugin panel a popup: 64 to 4096 logical pixels per
// axis. The tighter bound is the contract, not the wire's MaxExtent.
func boundPanelResize(p *v1.PanelResizeParams) error {
	if p.Width < minPanelExtent || p.Width > maxPanelExtent {
		return fmt.Errorf("panel width %d is outside %d..%d", p.Width, minPanelExtent, maxPanelExtent)
	}
	if p.Height < minPanelExtent || p.Height > maxPanelExtent {
		return fmt.Errorf("panel height %d is outside %d..%d", p.Height, minPanelExtent, maxPanelExtent)
	}
	return nil
}

const (
	maxNotifyText    = 16 << 10
	maxNotifyActions = 6
)

func boundNotify(p *v1.NotifyParams) error {
	if len(p.Summary) > maxNotifyText {
		return fmt.Errorf("notify summary exceeds %d bytes", maxNotifyText)
	}
	if len(p.Body) > maxNotifyText {
		return fmt.Errorf("notify body exceeds %d bytes", maxNotifyText)
	}
	if len(p.Actions) > maxNotifyActions {
		return fmt.Errorf("notify actions exceed %d", maxNotifyActions)
	}
	for _, a := range p.Actions {
		if len(a.Key)+len(a.Label) > maxNotifyText {
			return fmt.Errorf("notify action exceeds %d bytes", maxNotifyText)
		}
	}
	if p.TimeoutMS < 0 {
		return fmt.Errorf("notify timeout %d is negative", p.TimeoutMS)
	}
	return nil
}

func decodeParams(raw json.RawMessage, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	return nil
}

func decodeStrictParams(raw json.RawMessage, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("params: multiple JSON values")
		}
		return fmt.Errorf("params: %w", err)
	}
	return nil
}

func okReply(id string, result any) v1.HostReply {
	reply := v1.HostReply{ID: id, OK: true}
	if result == nil {
		return reply
	}
	data, err := json.Marshal(result)
	if err != nil {
		return failReply(id, err.Error())
	}
	reply.Result = data
	return reply
}

func failReply(id, err string) v1.HostReply {
	return v1.HostReply{ID: id, Error: err}
}
