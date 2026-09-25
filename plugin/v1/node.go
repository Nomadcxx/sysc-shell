// Package v1 is the language-neutral wire contract between the shell's plugin
// host and a plugin process. It holds the JSON Lines message set, the
// declarative view tree, and the bounds both sides enforce.
//
// The package deliberately imports nothing from the shell. A plugin is an
// ordinary process that speaks JSON on standard output, so the wire, not this
// Go package, is the compatibility surface: the committed JSON fixtures decide
// what version one means, and a plugin written in another language that
// produces the same bytes is equally valid.
//
// Nothing here describes rendering. The tree carries what a plugin means, and
// the host decides what that looks like: no bounds, callbacks, focus indexes,
// IME state, colours outside the theme's own roles, or renderer objects cross
// this boundary.
package v1

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// The version-one ceilings. They are constants rather than configuration
// because both sides must agree on them without negotiating, and because a
// limit a plugin could raise would not be a limit.
const (
	// MaxNodes counts the root, so a tree is at most MaxNodes nodes in total.
	MaxNodes = 1024
	// MaxDepth counts the root as level one.
	MaxDepth = 16
	// MaxChildren bounds one node's child list.
	MaxChildren = 256
	// MaxTextBytes bounds the text a single node carries.
	MaxTextBytes = 64 << 10
	// MaxInputBytes gives editable note bodies a larger, still bounded payload.
	MaxInputBytes = 1 << 20
	// MaxIdentBytes bounds node IDs, keys, icon names, accessible names, and
	// roles. These are addresses and labels, not content.
	MaxIdentBytes = 256
	// MaxExtent bounds any logical-pixel measurement on the wire. No output
	// this shell supports is wider than this, so a larger number is a mistake
	// or an attempt to make layout expensive.
	MaxExtent = 8192
	// MaxRadius bounds the corner-radius override. Cards and chips live in
	// the low double digits; anything larger is a mistake or an attack on
	// the rasteriser.
	MaxRadius = 256
	// MaxTooltipBytes bounds the hover text one node carries. A tooltip is a
	// glanceable hint, not a document.
	MaxTooltipBytes = 256
	// MinGraphSamples and MaxGraphSamples bound a graph's sample count.
	// One sample is a dot, not a shape; more than this is a data dump the
	// host cannot draw meaningfully at view sizes.
	MinGraphSamples = 2
	MaxGraphSamples = 64
	// MaxPathBytes bounds an image node's filesystem path. The host reads
	// the file itself, so the path is an address, not content.
	MaxPathBytes = 4096
	// MaxStroke bounds a container or button rim in logical pixels. The
	// consumer is a one-pixel hairline; anything larger is a mistake.
	MaxStroke         = 8
	MaxScheduleEvents = 256
	// MaxIconSize bounds an icon's explicit square in logical pixels. A
	// panel hero glyph is the consumer; past this a glyph is a wallpaper,
	// and a rasterisation that large is an attack on the paint budget.
	MaxIconSize = 256
	// MinSpriteFrames and MaxSpriteFrames bound a sprite cycle's poses. One
	// pose is a still icon; past this a cycle is a video the glyph catalogue
	// was never meant to carry.
	MinSpriteFrames = 2
	MaxSpriteFrames = 32
	// MinCycleMS and MaxCycleMS bound one pass through a sprite cycle. Faster
	// than this, a pose lasts under a frame at 60 Hz for a full cycle; slower,
	// the motion stops reading as motion.
	MinCycleMS = 100
	MaxCycleMS = 60000
)

type ScheduleGrid struct {
	Start    time.Time       `json:"start"`
	Now      time.Time       `json:"now"`
	Zone     string          `json:"zone"`
	Days     int             `json:"days"`
	Selected string          `json:"selected,omitempty"`
	Events   []ScheduleEvent `json:"events"`
}

type ScheduleEvent struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Name      string    `json:"name"`
	Start     time.Time `json:"start,omitempty"`
	End       time.Time `json:"end,omitempty"`
	AllDay    bool      `json:"all_day,omitempty"`
	StartDate string    `json:"start_date,omitempty"`
	EndDate   string    `json:"end_date,omitempty"`
	Marker    string    `json:"marker,omitempty"`
}

// NodeKind names a view element. Kinds are strings so that a plugin written in
// any language is self-describing on the wire and an unknown kind produces a
// diagnosable error rather than a silently different element.
type NodeKind string

const (
	KindRow        NodeKind = "row"
	KindColumn     NodeKind = "column"
	KindText       NodeKind = "text"
	KindIcon       NodeKind = "icon"
	KindProgress   NodeKind = "progress"
	KindButton     NodeKind = "button"
	KindTextInput  NodeKind = "text_input"
	KindList       NodeKind = "list"
	KindDragSource NodeKind = "drag_source"
	KindDropZone   NodeKind = "drop_zone"
	KindGauge      NodeKind = "gauge"
	// KindGraph draws a column sparkline of normalized samples, oldest
	// first. It is the plugin's history at a glance: values carry the data,
	// absent reserves the box when nothing has been recorded yet.
	KindGraph NodeKind = "graph"
	// KindSeparator is a rhythm rule between sibling groups. It is a panel
	// affordance: a bar strip has no room for punctuation.
	KindSeparator NodeKind = "separator"
	// KindImage displays a host-decoded raster from an absolute filesystem
	// path. It is a panel affordance: bar slots are fixed-width and rebuilt
	// per frame, so a decode job per bar revision is waste no consumer
	// needs. The host owns the decode; the plugin names a path and gains
	// display, not read, power.
	KindImage NodeKind = "image"
	// KindSegmented exposes the shell's exclusive segmented control to plugins.
	KindSegmented NodeKind = "segmented"
	// KindScheduleGrid arranges bounded event occurrences in local-time columns.
	KindScheduleGrid NodeKind = "schedule_grid"
)

// EventKind names an input event a node declares it can emit. A node receives
// only the kinds it declares, which is what lets the host derive hit testing
// and focus order without asking the plugin.
type EventKind string

const (
	EventActivate EventKind = "activate"
	EventPointer  EventKind = "pointer"
	EventChange   EventKind = "change"
	EventSubmit   EventKind = "submit"
	EventScroll   EventKind = "scroll"
	EventDrop     EventKind = "drop"
	// EventShortcut is a panel-level action, not a node-emitted pointer event.
	EventShortcut EventKind = "shortcut"
)

// Tone selects the semantic theme role a text node paints in. Plugins name
// roles rather than colours so that a plugin cannot break contrast or ignore
// the user's palette.
type Tone string

const (
	ToneNormal Tone = ""
	ToneError  Tone = "error"
	// ToneSubtle paints secondary text -- labels beside a value, captions,
	// out-of-month calendar days -- in the muted foreground the theme derives
	// from on_surface_variant. It is a hierarchy signal, never a state: a
	// failure stays ToneError.
	ToneSubtle Tone = "subtle"
	// ToneAccent paints emphasized text -- a running countdown, the active
	// player's name -- in the theme accent at full contrast.
	ToneAccent Tone = "accent"
)

// ViewKind names where a tree is going to be shown. The same vocabulary is not
// legal everywhere: a bar strip has no keyboard focus and a tooltip is not
// interactive at all, so the kind is part of validation rather than a hint.
type ViewKind string

const (
	ViewBar      ViewKind = "bar"
	ViewTooltip  ViewKind = "tooltip"
	ViewPanel    ViewKind = "panel"
	ViewFloating ViewKind = "floating"
)

// Node is one declarative view element.
//
// Every field is data the plugin chose. Arranged bounds, focus state, hover,
// pressed state, the live text buffer, and preedit belong to the host and have
// no representation here.
//
// Fill, Radius, Bold, Size, Disabled, CenterX, and PinEnd arrived in protocol
// minor two. Tooltip, Shape, Values, Absent, and the graph and separator kinds
// arrived in minor four. Path, the image box fields, Background, Stroke, and
// StrokeFill, and the image kind arrived in minor five. Animate arrived in
// minor six. Minor seven widened Absent to meters. IconSize, Frames, and
// CycleMS arrived in minor eight. A minor-one host ignores the new fields,
// so a plugin that sets them still speaks to an older shell,
// just without the presentation.
type Node struct {
	Kind NodeKind `json:"kind"`

	// ID addresses this node in input events. It is required on interactive
	// nodes and must be unique within a view revision.
	ID string `json:"id,omitempty"`
	// Key is stable identity across revisions. The host uses it to keep
	// retained state, such as a live editor buffer, attached to the same
	// element when the surrounding tree changes.
	Key string `json:"key,omitempty"`

	// Text is a label on a button, the content of a text node, and the
	// plugin's view of a field's value.
	Text string `json:"text,omitempty"`
	// Icon names a symbol from the shell's catalogue. It is a name rather than
	// a codepoint or a path so that the shell owns which glyphs exist.
	Icon string `json:"icon,omitempty"`
	// IconSize fixes an icon node's square in logical pixels, so a glyph can
	// stand as a panel's hero rather than only as a label beside text. Zero
	// keeps the host's own size. The host still paints it in the node's tone:
	// a size is geometry, never colour. It arrived with protocol minor eight.
	IconSize int `json:"icon_size,omitempty"`
	// Frames and CycleMS make a sized icon a sprite cycle: the host steps
	// through Frames, catalogue names in order, once every CycleMS on its own
	// frame clock, so the plugin publishes a pose list and a period rather
	// than a patch per pose. A name may repeat where the motion returns
	// through a pose. Icon stays the resting pose, painted under reduced
	// motion. Both need IconSize and a Key, and each needs the other. They
	// arrived with protocol minor eight.
	Frames  []string `json:"frames,omitempty"`
	CycleMS int      `json:"cycle_ms,omitempty"`
	// Value is a progress fraction from zero through one.
	Value float64 `json:"value,omitempty"`
	// Animate asks the host to glide this node's Value to each new revision's
	// target instead of jumping. Progress and gauge only, and it requires a
	// Key so the host can keep one transition attached to one element across
	// revisions. It arrived with protocol minor six.
	Animate bool `json:"animate,omitempty"`
	// ValueText is the gauge's centre label, such as a remaining time. An
	// empty value falls back to a percentage.
	ValueText string `json:"value_text,omitempty"`

	// Tone selects semantic presentation.
	Tone Tone `json:"tone,omitempty"`
	// Fill names the semantic background of a container or button. The host
	// maps it onto its own theme tokens; an unknown name is a diagnosable
	// validation error, not a fallback.
	Fill string `json:"fill,omitempty"`
	// Radius overrides the corner radius of a container in logical pixels.
	Radius int `json:"radius,omitempty"`
	// Bold marks an emphasized text run. Shaping resolves a real bold face.
	Bold bool `json:"bold,omitempty"`
	// Size names a type-ladder rung: body, caption, label, title, headline,
	// display, mono. The theme decides the point size and weight.
	Size string `json:"size,omitempty"`
	// Disabled greys an interactive node out: it stays in keyboard traversal
	// with its accessible name explaining why, and activation is blocked.
	Disabled bool `json:"disabled,omitempty"`
	// Selected marks the active button inside a segmented control. Minor eight.
	Selected bool `json:"selected,omitempty"`
	// MarkerColor supplies an event's bounded RGB marker. The shell uses it
	// only on the button rim, never on text or the event surface. Minor eight.
	MarkerColor string        `json:"marker_color,omitempty"`
	Schedule    *ScheduleGrid `json:"schedule,omitempty"`
	// CenterX centres a child in its column track.
	CenterX bool `json:"center_x,omitempty"`
	// PinEnd right-pins the last child of a two-child row.
	PinEnd bool `json:"pin_end,omitempty"`
	// Tabular requests fixed-advance figures. A countdown sets it: with
	// proportional digits the rendered width changes every second, which
	// visibly shifts everything beside it.
	Tabular bool `json:"tabular,omitempty"`

	// Tooltip is bounded hover text owned by the node's feature. The shell
	// paints it wherever it paints its own hints; a longer value is a
	// diagnosable validation error.
	Tooltip string `json:"tooltip,omitempty"`
	// Shape names the corner treatment a container or button asks for:
	// circle, stadium, small, medium, large, card, panel. The host maps it
	// onto its own shape tokens; an unknown name is a diagnosable error.
	Shape string `json:"shape,omitempty"`
	// Values are the graph's samples, oldest first, each normalized zero
	// through one. Only a graph carries them.
	Values []float64 `json:"values,omitempty"`
	// Absent reserves the node's box and paints nothing: a gauge with no
	// reading yet keeps the layout steady instead of vanishing. A meter
	// gained the same power in minor seven — an elapsed strip with unknown
	// window bounds reserves its slot honestly. Only a gauge, a graph, or
	// a meter may be absent.
	Absent bool `json:"absent,omitempty"`

	// Path names the absolute file an image node displays. The host decodes
	// it with its own caps and cache; the plugin gains display, not read,
	// power. A relative path would resolve against the shell's working
	// directory, which the plugin cannot know, so it is a validation error.
	Path string `json:"path,omitempty"`
	// ImageSize fixes a square image edge in logical pixels; ImageW and
	// ImageH fix an explicit box instead. Exactly one form is legal, and
	// one of a pair alone is rejected rather than silently squared.
	ImageSize int `json:"image_size,omitempty"`
	ImageW    int `json:"image_w,omitempty"`
	ImageH    int `json:"image_h,omitempty"`
	// Background asks the painter to cover-fill the explicit box rather
	// than fit the image inside it. A cover-fill needs a container shape
	// to fill, so it requires the explicit box.
	Background bool `json:"background,omitempty"`
	// Stroke draws a rim around a row, column, or button, in logical
	// pixels; StrokeFill names the rim's fill and defaults to the outline
	// tone when absent.
	Stroke     int    `json:"stroke,omitempty"`
	StrokeFill string `json:"stroke_fill,omitempty"`

	// Width fixes a logical width; MaxWidth caps a measured one. Padding and
	// Gap open a container. Zero means natural in every case.
	Width    int `json:"width,omitempty"`
	Height   int `json:"height,omitempty"`
	MaxWidth int `json:"max_width,omitempty"`
	Padding  int `json:"padding,omitempty"`
	Gap      int `json:"gap,omitempty"`

	// DragType is the payload class a drag source offers. Accept is the set a
	// drop zone will take. Payload is the value delivered on drop.
	DragType string   `json:"drag_type,omitempty"`
	Accept   []string `json:"accept,omitempty"`
	Payload  string   `json:"payload,omitempty"`

	// Name and Role are the accessible identity. They are required on every
	// interactive node: the host derives the accessibility tree from the
	// plugin's declaration and has no other source for it.
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
	// Events are the input kinds this node emits. An empty list on an
	// interactive node is a mistake, not a read-only element.
	Events []EventKind `json:"events,omitempty"`

	// Multiline and SubmitOnEnter apply only to text inputs. The host owns the
	// live buffer; Reseed is the generation the plugin uses to replace it.
	Multiline     bool   `json:"multiline,omitempty"`
	SubmitOnEnter bool   `json:"submit_on_enter,omitempty"`
	Reseed        uint64 `json:"reseed,omitempty"`
	// Placeholder is presentation copy shown only while a text input is empty.
	// Name remains the accessible label.
	Placeholder string `json:"placeholder,omitempty"`

	Children []*Node `json:"children,omitempty"`
}

// container reports whether a kind holds children.
func (k NodeKind) container() bool {
	return k == KindRow || k == KindColumn || k == KindList || k == KindDropZone || k == KindSegmented
}

// interactive reports whether a kind produces input and therefore needs an
// address and an accessible identity.
func (k NodeKind) interactive() bool {
	return k == KindButton || k == KindTextInput || k == KindDragSource
}

// keyboard reports whether a kind needs keyboard focus to be usable. Bar views
// have no keyboard focus, so placing one there would render a control the user
// could never reach.
func (k NodeKind) keyboard() bool { return k == KindTextInput }

// allowedEvents is the exact event set each kind may declare.
var allowedEvents = map[NodeKind]map[EventKind]bool{
	KindButton:     {EventActivate: true, EventPointer: true},
	KindTextInput:  {EventChange: true, EventSubmit: true},
	KindDragSource: {EventPointer: true, EventDrop: true},
	KindDropZone:   {EventDrop: true},
	KindList:       {EventScroll: true},
}

var knownKinds = map[NodeKind]bool{
	KindRow: true, KindColumn: true, KindText: true, KindIcon: true,
	KindProgress: true, KindButton: true, KindTextInput: true,
	KindList: true, KindDragSource: true, KindDropZone: true,
	KindGauge: true, KindGraph: true, KindSeparator: true, KindImage: true,
	KindSegmented: true, KindScheduleGrid: true,
}

var knownViews = map[ViewKind]bool{ViewBar: true, ViewTooltip: true, ViewPanel: true, ViewFloating: true}

var knownTones = map[Tone]bool{ToneNormal: true, ToneError: true, ToneSubtle: true, ToneAccent: true}

var knownFills = map[string]bool{
	"surface": true, "accent": true, "container": true, "error": true,
	"soft": true, "card": true, "outline": true, "chip": true,
	"error-container": true, "note-sun": true, "note-mint": true,
	"note-sky": true, "note-rose": true, "note-lilac": true,
}

var knownSizes = map[string]bool{
	"body": true, "caption": true, "label": true, "title": true,
	"headline": true, "display": true, "mono": true,
}

var knownShapes = map[string]bool{
	"circle": true, "stadium": true, "small": true, "medium": true,
	"large": true, "card": true, "panel": true,
}

func fillAllowed(k NodeKind) bool {
	return k.container() || k == KindButton
}

func noteFill(fill string) bool {
	switch fill {
	case "note-sun", "note-mint", "note-sky", "note-rose", "note-lilac":
		return true
	default:
		return false
	}
}

// Validate reports whether root is a legal version-one tree for the given view.
//
// It is the only gate between plugin JSON and anything the shell will convert,
// lay out, or paint, so it checks structure, bounds, addressing, accessibility,
// and view-specific vocabulary in one pass. An error names the offending path
// so a plugin author can find the node without instrumenting the host.
func Validate(root *Node, view ViewKind) error {
	if !knownViews[view] {
		return fmt.Errorf("unknown view kind %q", view)
	}
	if root == nil {
		return fmt.Errorf("view has no root node")
	}
	v := &validator{
		view: view,
		ids:  make(map[string]bool),
		keys: make(map[string]bool),
	}
	return v.node(root, "root", 1)
}

type validator struct {
	view  ViewKind
	ids   map[string]bool
	keys  map[string]bool
	nodes int
}

func (v *validator) node(n *Node, path string, depth int) error {
	return v.nodeIn(n, path, depth, false)
}

func (v *validator) nodeIn(n *Node, path string, depth int, segmentChild bool) error {
	if n == nil {
		return fmt.Errorf("%s: nil node", path)
	}
	if depth > MaxDepth {
		return fmt.Errorf("%s: tree deeper than %d levels", path, MaxDepth)
	}
	v.nodes++
	if v.nodes > MaxNodes {
		return fmt.Errorf("%s: view holds more than %d nodes", path, MaxNodes)
	}
	if !knownKinds[n.Kind] {
		return fmt.Errorf("%s: unknown kind %q", path, n.Kind)
	}
	if err := v.identity(n, path); err != nil {
		return err
	}
	if err := v.presentation(n, path); err != nil {
		return err
	}
	if err := v.vocabulary(n, path); err != nil {
		return err
	}
	if err := v.events(n, path); err != nil {
		return err
	}
	if err := v.minorTwo(n, path); err != nil {
		return err
	}
	if err := v.minorFour(n, path); err != nil {
		return err
	}
	if err := v.minorFive(n, path); err != nil {
		return err
	}
	if err := v.minorSix(n, path); err != nil {
		return err
	}
	if err := v.minorEight(n, path); err != nil {
		return err
	}
	if err := v.minorNine(n, path); err != nil {
		return err
	}
	if n.Selected && (!segmentChild || n.Kind != KindButton) {
		return fmt.Errorf("%s: selected is only valid on a button inside a segmented control", path)
	}
	if n.Kind == KindSegmented {
		if len(n.Children) < 2 {
			return fmt.Errorf("%s: segmented control needs at least two buttons", path)
		}
		selected := 0
		for i, child := range n.Children {
			if child == nil || child.Kind != KindButton {
				return fmt.Errorf("%s.children[%d]: segmented control requires buttons", path, i)
			}
			if child.Selected {
				selected++
			}
		}
		if selected != 1 {
			return fmt.Errorf("%s: segmented control has %d selected buttons, want one", path, selected)
		}
	}

	if !n.Kind.container() && n.Kind != KindButton && len(n.Children) > 0 {
		return fmt.Errorf("%s: %s takes no children", path, n.Kind)
	}
	if len(n.Children) > MaxChildren {
		return fmt.Errorf("%s: %d children, more than the %d allowed", path, len(n.Children), MaxChildren)
	}
	for i, c := range n.Children {
		if err := v.nodeIn(c, fmt.Sprintf("%s.children[%d]", path, i), depth+1, n.Kind == KindSegmented); err != nil {
			return err
		}
	}
	return nil
}

// identity checks addressing and accessibility.
func (v *validator) identity(n *Node, path string) error {
	for _, f := range []struct{ what, value string }{
		{"id", n.ID}, {"key", n.Key}, {"name", n.Name}, {"role", n.Role}, {"icon", n.Icon},
	} {
		if len(f.value) > MaxIdentBytes {
			return fmt.Errorf("%s: %s is %d bytes, more than the %d allowed", path, f.what, len(f.value), MaxIdentBytes)
		}
	}
	if n.ID != "" {
		if v.ids[n.ID] {
			return fmt.Errorf("%s: node id %q is already used in this view", path, n.ID)
		}
		v.ids[n.ID] = true
	}
	if n.Key != "" {
		if v.keys[n.Key] {
			return fmt.Errorf("%s: key %q is already used in this view", path, n.Key)
		}
		v.keys[n.Key] = true
	}
	if !n.Kind.interactive() {
		return nil
	}
	if n.ID == "" {
		return fmt.Errorf("%s: %s needs an id to receive input", path, n.Kind)
	}
	if n.Name == "" || n.Role == "" {
		return fmt.Errorf("%s: %s needs an accessible name and role", path, n.Kind)
	}
	return nil
}

// presentation checks the theme roles and every measurement.
func (v *validator) presentation(n *Node, path string) error {
	if !knownTones[n.Tone] {
		return fmt.Errorf("%s: unknown tone %q", path, n.Tone)
	}
	for _, f := range []struct {
		what string
		v    int
	}{{"width", n.Width}, {"height", n.Height}, {"max_width", n.MaxWidth}, {"padding", n.Padding}, {"gap", n.Gap}} {
		if f.v < 0 {
			return fmt.Errorf("%s: %s is negative", path, f.what)
		}
		if f.v > MaxExtent {
			return fmt.Errorf("%s: %s is %d, past the %d limit", path, f.what, f.v, MaxExtent)
		}
	}
	return nil
}

// vocabulary checks the per-kind payload and what this view kind permits.
func (v *validator) vocabulary(n *Node, path string) error {
	maxTextBytes := MaxTextBytes
	if n.Kind == KindTextInput {
		maxTextBytes = MaxInputBytes
	}
	if len(n.Text) > maxTextBytes {
		return fmt.Errorf("%s: text is %d bytes, more than the %d allowed", path, len(n.Text), maxTextBytes)
	}
	if len(n.Placeholder) > MaxTextBytes {
		return fmt.Errorf("%s: placeholder is %d bytes, more than the %d allowed", path, len(n.Placeholder), MaxTextBytes)
	}
	switch n.Kind {
	case KindIcon:
		if err := icon(n.Icon); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	case KindButton:
		if n.Icon != "" {
			if err := icon(n.Icon); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	case KindProgress:
		if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) {
			return fmt.Errorf("%s: progress value is not finite", path)
		}
		if n.Value < 0 || n.Value > 1 {
			return fmt.Errorf("%s: progress value %v is outside zero through one", path, n.Value)
		}
	case KindGauge:
		if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) {
			return fmt.Errorf("%s: gauge value is not finite", path)
		}
		if n.Value < 0 || n.Value > 1 {
			return fmt.Errorf("%s: gauge value %v is outside zero through one", path, n.Value)
		}
		if len(n.ValueText) > MaxTextBytes {
			return fmt.Errorf("%s: value text is %d bytes, more than the %d allowed", path, len(n.ValueText), MaxTextBytes)
		}
	}
	if v.view == ViewTooltip && n.Kind.interactive() {
		return fmt.Errorf("%s: a tooltip is read-only and cannot hold a %s", path, n.Kind)
	}
	if n.Kind == KindSegmented && v.view != ViewPanel {
		return fmt.Errorf("%s: a segmented control is only valid in a panel", path)
	}
	if v.view == ViewBar && n.Kind.keyboard() {
		return fmt.Errorf("%s: a bar view has no keyboard focus and cannot hold a %s", path, n.Kind)
	}
	if v.view != ViewPanel && v.view != ViewFloating && (n.Kind == KindList || n.Kind == KindDragSource || n.Kind == KindDropZone || n.Kind == KindSeparator) {
		return fmt.Errorf("%s: a %s view cannot hold a %s", path, v.view, n.Kind)
	}
	if n.Kind == KindDragSource && n.Name == "" {
		return fmt.Errorf("%s: a drag handle needs an accessible name", path)
	}
	if n.Kind != KindTextInput && (n.Multiline || n.SubmitOnEnter || n.Reseed != 0 || n.Placeholder != "") {
		return fmt.Errorf("%s: %s cannot carry editor flags", path, n.Kind)
	}
	return nil
}

// events checks that a node declares a legal, non-empty, duplicate-free set.
func (v *validator) events(n *Node, path string) error {
	allowed := allowedEvents[n.Kind]
	if len(allowed) == 0 && len(n.Events) > 0 {
		return fmt.Errorf("%s: %s emits no events", path, n.Kind)
	}
	if n.Kind.interactive() && len(n.Events) == 0 {
		return fmt.Errorf("%s: %s declares no events and could never be used", path, n.Kind)
	}
	seen := make(map[EventKind]bool, len(n.Events))
	for _, e := range n.Events {
		if !allowed[e] {
			return fmt.Errorf("%s: %s cannot emit %q", path, n.Kind, e)
		}
		if seen[e] {
			return fmt.Errorf("%s: event %q declared twice", path, e)
		}
		seen[e] = true
	}
	return nil
}

// minorTwo checks the minor-version-two presentation fields: the fill and
// size vocabularies, the radius bound, and the kinds each field may ride on.
func (v *validator) minorTwo(n *Node, path string) error {
	if n.Fill != "" {
		if !knownFills[n.Fill] {
			return fmt.Errorf("%s: unknown fill %q", path, n.Fill)
		}
		if !fillAllowed(n.Kind) {
			return fmt.Errorf("%s: %s cannot carry a fill", path, n.Kind)
		}
		if noteFill(n.Fill) && v.view != ViewFloating {
			return fmt.Errorf("%s: sticky note fills are only available in a floating view", path)
		}
	}
	if n.Radius < 0 || n.Radius > MaxRadius {
		return fmt.Errorf("%s: radius is %d, past the %d limit", path, n.Radius, MaxRadius)
	}
	if n.Size != "" {
		if !knownSizes[n.Size] {
			return fmt.Errorf("%s: unknown size %q", path, n.Size)
		}
		if n.Kind != KindText {
			return fmt.Errorf("%s: %s cannot carry a size", path, n.Kind)
		}
	}
	if n.Disabled && !n.Kind.interactive() {
		return fmt.Errorf("%s: %s cannot be disabled", path, n.Kind)
	}
	return nil
}

// minorFour validates the parity surface that arrived with protocol minor
// four: hover tooltips, semantic shapes, graph sparklines, separators, and
// the absent dim state. Each maps onto a renderer the host already ships,
// so the checks stay vocabulary-level.
func (v *validator) minorFour(n *Node, path string) error {
	if len(n.Tooltip) > MaxTooltipBytes {
		return fmt.Errorf("%s: tooltip is %d bytes, more than the %d allowed", path, len(n.Tooltip), MaxTooltipBytes)
	}
	if n.Shape != "" {
		if !knownShapes[n.Shape] {
			return fmt.Errorf("%s: unknown shape %q", path, n.Shape)
		}
		if !fillAllowed(n.Kind) && n.Kind != KindImage {
			return fmt.Errorf("%s: %s cannot carry a shape", path, n.Kind)
		}
	}
	if len(n.Values) > 0 {
		if n.Kind != KindGraph {
			return fmt.Errorf("%s: %s cannot carry values", path, n.Kind)
		}
		if len(n.Values) < MinGraphSamples || len(n.Values) > MaxGraphSamples {
			return fmt.Errorf("%s: graph holds %d samples, outside %d through %d", path, len(n.Values), MinGraphSamples, MaxGraphSamples)
		}
		for i, s := range n.Values {
			if math.IsNaN(s) || math.IsInf(s, 0) {
				return fmt.Errorf("%s: graph sample %d is not finite", path, i)
			}
			if s < 0 || s > 1 {
				return fmt.Errorf("%s: graph sample %d is outside zero through one", path, i)
			}
		}
	}
	// Minor seven widened absent from gauge to the other value-bearing
	// kinds: an elapsed strip with unknown window bounds must reserve its
	// slot and paint nothing, not quietly claim the window just began.
	if n.Absent && n.Kind != KindGauge && n.Kind != KindGraph && n.Kind != KindProgress {
		return fmt.Errorf("%s: %s cannot be absent", path, n.Kind)
	}
	return nil
}

// minorFive validates the presentation primitives that arrived with protocol
// minor five: the image kind with its host-decoded path and box forms, the
// container stroke, and explicit button children. Each maps onto a painter
// the host already ships, so the checks stay vocabulary-level.
func (v *validator) minorFive(n *Node, path string) error {
	if n.Kind == KindImage {
		if v.view != ViewPanel && v.view != ViewFloating {
			return fmt.Errorf("%s: a %s view cannot hold an image", path, v.view)
		}
		if n.Path == "" {
			return fmt.Errorf("%s: image has no path", path)
		}
		if n.Path[0] != '/' {
			return fmt.Errorf("%s: image path %q is not absolute", path, n.Path)
		}
		if len(n.Path) > MaxPathBytes {
			return fmt.Errorf("%s: image path is %d bytes, more than the %d allowed", path, len(n.Path), MaxPathBytes)
		}
		square, boxed := n.ImageSize > 0, n.ImageW > 0 || n.ImageH > 0
		if square && boxed {
			return fmt.Errorf("%s: image sets image_size and an explicit box; exactly one form is legal", path)
		}
		if !square && !boxed {
			return fmt.Errorf("%s: image needs an image_size or an image_w and image_h box", path)
		}
		if square {
			if n.ImageSize > MaxExtent {
				return fmt.Errorf("%s: image_size is %d, past the %d limit", path, n.ImageSize, MaxExtent)
			}
		} else {
			if n.ImageW <= 0 || n.ImageH <= 0 {
				return fmt.Errorf("%s: image box needs both image_w and image_h, not one of the pair", path)
			}
			if n.ImageW > MaxExtent || n.ImageH > MaxExtent {
				return fmt.Errorf("%s: image box is past the %d limit", path, MaxExtent)
			}
		}
		if n.Background && !(n.ImageW > 0 && n.ImageH > 0) {
			return fmt.Errorf("%s: image background needs the explicit image_w and image_h box", path)
		}
	} else if n.Path != "" || n.ImageSize != 0 || n.ImageW != 0 || n.ImageH != 0 || n.Background {
		return fmt.Errorf("%s: %s cannot carry image fields", path, n.Kind)
	}
	if n.Stroke != 0 || n.StrokeFill != "" {
		if n.Kind != KindRow && n.Kind != KindColumn && n.Kind != KindButton {
			return fmt.Errorf("%s: %s cannot carry a stroke", path, n.Kind)
		}
		if n.Stroke < 0 || n.Stroke > MaxStroke {
			return fmt.Errorf("%s: stroke is %d, outside zero through %d", path, n.Stroke, MaxStroke)
		}
		if n.StrokeFill != "" && !knownFills[n.StrokeFill] {
			return fmt.Errorf("%s: unknown stroke fill %q", path, n.StrokeFill)
		}
		if noteFill(n.StrokeFill) && v.view != ViewFloating {
			return fmt.Errorf("%s: sticky note fills are only available in a floating view", path)
		}
	}
	if n.Kind == KindButton {
		for i, c := range n.Children {
			if c.Kind.interactive() {
				return fmt.Errorf("%s: button child %d is an interactive %s; the button is the one hit target", path, i, c.Kind)
			}
		}
	}
	return nil
}

// minorSix validates the declarative value animation that arrived with
// protocol minor six: the animate flag, legal only on the two value kinds and
// only when the node carries a key for the host's animator to hold onto.
func (v *validator) minorSix(n *Node, path string) error {
	if !n.Animate {
		return nil
	}
	if n.Kind != KindProgress && n.Kind != KindGauge {
		return fmt.Errorf("%s: %s cannot animate", path, n.Kind)
	}
	if n.Key == "" {
		return fmt.Errorf("%s: an animated %s needs a key so the host keeps one transition across revisions", path, n.Kind)
	}
	return nil
}

func (v *validator) minorEight(n *Node, path string) error {
	if n.MarkerColor != "" && (n.Kind != KindButton || !validRGBMarker(n.MarkerColor)) {
		return fmt.Errorf("%s: marker color is only valid as RGB on a button", path)
	}
	if n.Schedule == nil {
		if n.Kind == KindScheduleGrid {
			return fmt.Errorf("%s: schedule grid needs schedule data", path)
		}
		return nil
	}
	if n.Kind != KindScheduleGrid {
		return fmt.Errorf("%s: %s cannot carry schedule data", path, n.Kind)
	}
	if v.view != ViewPanel {
		return fmt.Errorf("%s: a schedule grid is only valid in a panel", path)
	}
	s := n.Schedule
	if s.Start.IsZero() || s.Now.IsZero() {
		return fmt.Errorf("%s: schedule start and current time are required", path)
	}
	if s.Days < 1 || s.Days > 7 {
		return fmt.Errorf("%s: schedule days %d is outside one through seven", path, s.Days)
	}
	if len(s.Zone) == 0 || len(s.Zone) > 64 {
		return fmt.Errorf("%s: schedule time zone is invalid", path)
	}
	zone, err := time.LoadLocation(s.Zone)
	if err != nil {
		return fmt.Errorf("%s: schedule time zone %q is unknown", path, s.Zone)
	}
	start := s.Start.In(zone)
	year, month, day := start.Date()
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 || !s.Start.Equal(time.Date(year, month, day, 0, 0, 0, 0, zone)) {
		return fmt.Errorf("%s: schedule start must be local midnight", path)
	}
	if len(s.Events) > MaxScheduleEvents {
		return fmt.Errorf("%s: schedule has %d events, more than %d", path, len(s.Events), MaxScheduleEvents)
	}
	seen := make(map[string]bool, len(s.Events))
	selected := s.Selected == ""
	for i, event := range s.Events {
		eventPath := fmt.Sprintf("%s.schedule.events[%d]", path, i)
		if event.ID == "" || len(event.ID) > MaxIdentBytes || seen[event.ID] || v.ids[event.ID] {
			return fmt.Errorf("%s: event ID is empty, oversized, or duplicated", eventPath)
		}
		seen[event.ID], v.ids[event.ID] = true, true
		if len(event.Title) == 0 || len(event.Title) > 512 || len(event.Name) == 0 || len(event.Name) > MaxIdentBytes {
			return fmt.Errorf("%s: title or accessible name is outside bounds", eventPath)
		}
		if !validScheduleMarker(event.Marker) {
			return fmt.Errorf("%s: unknown semantic marker or invalid RGB marker %q", eventPath, event.Marker)
		}
		if event.AllDay {
			first, firstErr := time.Parse("2006-01-02", event.StartDate)
			last, lastErr := time.Parse("2006-01-02", event.EndDate)
			if firstErr != nil || lastErr != nil || !first.Before(last) || !event.Start.IsZero() || !event.End.IsZero() {
				return fmt.Errorf("%s: all-day event needs valid exclusive date bounds", eventPath)
			}
		} else if event.Start.IsZero() || event.End.IsZero() || !event.Start.Before(event.End) || event.End.Sub(event.Start) > 366*24*time.Hour || event.StartDate != "" || event.EndDate != "" {
			return fmt.Errorf("%s: timed event needs a positive interval under one year", eventPath)
		}
		if event.ID == s.Selected {
			selected = true
		}
	}
	if !selected {
		return fmt.Errorf("%s: selected event %q is not present", path, s.Selected)
	}
	return nil
}

// minorNine validates the explicit icon size that arrived with protocol
// minor nine: legal only on an icon node, and bounded like every other
// measurement on the wire. Whether the square fits its view is geometry,
// which the host's layout decides and plugin/lint reports.
func (v *validator) minorNine(n *Node, path string) error {
	if n.IconSize == 0 && n.Frames == nil && n.CycleMS == 0 {
		return nil
	}
	if n.Kind != KindIcon {
		return fmt.Errorf("%s: %s cannot carry an icon size or a sprite cycle", path, n.Kind)
	}
	if n.IconSize < 0 || n.IconSize > MaxIconSize {
		return fmt.Errorf("%s: icon size is %d, outside 1 through %d", path, n.IconSize, MaxIconSize)
	}
	if n.Frames == nil && n.CycleMS == 0 {
		return nil
	}
	switch {
	case n.CycleMS == 0:
		return fmt.Errorf("%s: sprite frames need a cycle_ms", path)
	case n.Frames == nil:
		return fmt.Errorf("%s: a cycle_ms needs sprite frames", path)
	case n.IconSize == 0:
		return fmt.Errorf("%s: a sprite cycle needs an icon_size so its box does not change per pose", path)
	case n.Key == "":
		return fmt.Errorf("%s: a sprite cycle needs a key so the host keeps its phase across revisions", path)
	case len(n.Frames) < MinSpriteFrames || len(n.Frames) > MaxSpriteFrames:
		return fmt.Errorf("%s: sprite holds %d frames, outside %d through %d", path, len(n.Frames), MinSpriteFrames, MaxSpriteFrames)
	case n.CycleMS < MinCycleMS || n.CycleMS > MaxCycleMS:
		return fmt.Errorf("%s: cycle_ms is %d, outside %d through %d", path, n.CycleMS, MinCycleMS, MaxCycleMS)
	}
	for i, name := range n.Frames {
		if len(name) > MaxIdentBytes {
			return fmt.Errorf("%s: frame %d is %d bytes, more than the %d allowed", path, i, len(name), MaxIdentBytes)
		}
		if err := icon(name); err != nil {
			return fmt.Errorf("%s: frame %d: %w", path, i, err)
		}
	}
	return nil
}

func validScheduleMarker(marker string) bool {
	if marker == "accent" || marker == "secondary" || marker == "tertiary" || marker == "outline" {
		return true
	}
	return validRGBMarker(marker)
}

func validRGBMarker(marker string) bool {
	if len(marker) != 7 || marker[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(marker[1:], 16, 24)
	return err == nil
}

// icon checks that a name addresses the shell's catalogue rather than the
// filesystem. Resolution to a glyph belongs to the host; this only proves the
// name is an identifier and can never be a path.
func icon(name string) error {
	if name == "" {
		return fmt.Errorf("icon has no name")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		// Underscore is how the material subset spells its names
		// (play_arrow, restart_alt). It separates words without ever
		// naming a directory, which is what this check guards against.
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return fmt.Errorf("icon name %q is not a lower-case identifier", name)
		}
	}
	return nil
}
