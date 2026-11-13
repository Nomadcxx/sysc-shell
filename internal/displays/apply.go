package displays

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

const (
	// applyBudget bounds one apply-and-verify pass, well inside the armed
	// deadline, so a slow compositor cannot consume the whole preview window.
	applyBudget = 5 * time.Second
)

// armWindow is how long a preview may stay live after it is armed. It is a var
// only so the survival test can run the same code in seconds; production
// always uses the value below.
var armWindow = 20 * time.Second

// applier makes the running compositor match a policy and then proves it did.
// The strict path is a preview, where anything unexpected is a failure the user
// must see. The tolerant path is recovery, where a monitor that disappeared is
// recorded rather than treated as a broken restoration.
type applier struct {
	socket string
	strict bool
	notes  []string
}

func (a *applier) note(format string, args ...any) {
	a.notes = append(a.notes, fmt.Sprintf(format, args...))
}

func (a *applier) live(ctx context.Context) (map[string]niri.Output, error) {
	outs, err := niri.Outputs(ctx, a.socket)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]niri.Output, len(outs))
	for _, out := range outs {
		byName[out.Name] = out
	}
	return byName, nil
}

func sortedOutputs(live map[string]niri.Output) []niri.Output {
	outs := make([]niri.Output, 0, len(live))
	for _, out := range live {
		outs = append(outs, out)
	}
	sort.Slice(outs, func(i, j int) bool { return outs[i].Name < outs[j].Name })
	return outs
}

// expected reports an output the transaction counted on that the compositor no
// longer has, under the caller's policy for that case.
func (a *applier) expected(gone string) error {
	if a.strict {
		return fmt.Errorf("displays: %s is not connected, so the preview cannot be confirmed", gone)
	}
	a.note("%s is no longer connected, so its pre-preview settings were not restored", gone)
	return nil
}

// sameOutput is the identity check: the same connector is not enough, because a
// replaced monitor on that connector must not inherit the old monitor's
// settings.
func sameOutput(saved Name, out niri.Output) bool {
	return saved.Connector == out.Name &&
		strings.EqualFold(saved.Make, out.Make) &&
		strings.EqualFold(saved.Model, out.Model) &&
		strings.EqualFold(saved.Serial, out.Serial)
}

func findSaved(before []OutputState, connector string) (OutputState, bool) {
	for _, saved := range before {
		if saved.Name.Connector == connector {
			return saved, true
		}
	}
	return OutputState{}, false
}

// checkIdentities refuses a preview when the set of outputs moved under it. A
// new or replaced output means the arrangement the user is confirming is not
// the one they were shown.
func (a *applier) checkIdentities(live map[string]niri.Output, before []OutputState) error {
	if !a.strict {
		return nil
	}
	for _, out := range sortedOutputs(live) {
		saved, ok := findSaved(before, out.Name)
		switch {
		case !ok:
			return fmt.Errorf("displays: %s appeared while the transaction was open", out.Name)
		case !sameOutput(saved.Name, out):
			return fmt.Errorf("displays: %s was replaced while the transaction was open", out.Name)
		}
	}
	for _, saved := range before {
		if _, ok := live[saved.Name.Connector]; !ok {
			return a.expected(saved.Name.Connector)
		}
	}
	return nil
}

func (a *applier) send(ctx context.Context, out niri.Output, change niri.OutputChange) error {
	outcome, err := niri.SetOutput(ctx, a.socket, out.Name, change)
	if err != nil {
		return fmt.Errorf("displays: set %s: %w", out.Name, err)
	}
	if outcome != niri.OutcomeApplied {
		// OutputWasMissing means niri queued the setting for a later
		// connection. It is not proof that anything changed, so it never
		// counts as success.
		return fmt.Errorf("displays: set %s was queued rather than applied (%s)", out.Name, outcome)
	}
	return nil
}

// checkNoOverlap refuses a target whose pinned outputs would land on top of each
// other. Edge adjacency is a draft rule the UI checks with proposed sizes;
// overlap is a broken layout, so it is also checked here with the sizes the
// compositor actually produced.
func checkNoOverlap(live map[string]niri.Output, target map[string]Policy) error {
	rects := map[string]Rect{}
	for name, want := range target {
		if want.Position == nil {
			continue
		}
		out, ok := live[name]
		if !ok || out.Logical == nil {
			continue
		}
		rects[name] = Rect{
			Position: Position{X: out.Logical.X, Y: out.Logical.Y},
			Width:    int32(out.Logical.Width),
			Height:   int32(out.Logical.Height),
		}
	}
	names := make([]string, 0, len(rects))
	for name := range rects {
		names = append(names, name)
	}
	sort.Strings(names)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if err := validatePair(names[i], rects[names[i]], names[j], rects[names[j]]); err != nil {
				if strings.Contains(err.Error(), "overlap") {
					return err
				}
			}
		}
	}
	return nil
}

// run applies target to every output it names, in the design's phase order, and
// verifies the result. Geometry comes first because positions are expressed in
// logical pixels; the arrangement is checked against the sizes the compositor
// actually produced, and VRR last because it changes nothing about the layout.
func (a *applier) run(ctx context.Context, target map[string]Policy, before []OutputState) error {
	live, err := a.live(ctx)
	if err != nil {
		return err
	}
	if err := a.checkIdentities(live, before); err != nil {
		return err
	}
	names := make([]string, 0, len(target))
	for name := range target {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		out, ok := live[name]
		if !ok {
			if err := a.expected(name); err != nil {
				return err
			}
			continue
		}
		geometry, _, err := planOutput(out, target[name])
		if err != nil {
			return err
		}
		for _, change := range geometry {
			if err := a.send(ctx, out, change); err != nil {
				return err
			}
		}
	}

	// Re-query so arrangement and verification use the real logical sizes.
	live, err = a.live(ctx)
	if err != nil {
		return err
	}
	if err := checkNoOverlap(live, target); err != nil {
		return err
	}
	for _, name := range names {
		out, ok := live[name]
		if !ok {
			if err := a.expected(name); err != nil {
				return err
			}
			continue
		}
		_, layout, err := planOutput(out, target[name])
		if err != nil {
			return err
		}
		for _, change := range layout {
			if err := a.send(ctx, out, change); err != nil {
				return err
			}
		}
	}

	live, err = a.live(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		out, ok := live[name]
		if !ok {
			if err := a.expected(name); err != nil {
				return err
			}
			continue
		}
		if err := verifyOutput(out, target[name]); err != nil {
			return err
		}
	}
	return nil
}

// apply is the strict path: a preview must match exactly the arrangement the
// user confirmed.
func applyTarget(ctx context.Context, socket string, target map[string]Policy, before []OutputState) error {
	ctx, cancel := context.WithTimeout(ctx, applyBudget)
	defer cancel()
	return (&applier{socket: socket, strict: true}).run(ctx, target, before)
}

// restore puts back the pre-preview state of every output that is still
// connected and still the same monitor. It is the tolerant path: a vanished
// output is recorded in the returned notes instead of failing, because no
// command can bring a monitor back, but a command the compositor refused is a
// failure that must be reported rather than hidden.
func restore(ctx context.Context, socket string, before []OutputState) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, applyBudget)
	defer cancel()
	a := &applier{socket: socket}
	live, err := a.live(ctx)
	if err != nil {
		return a.notes, err
	}
	target := map[string]Policy{}
	for _, saved := range before {
		out, ok := live[saved.Name.Connector]
		switch {
		case !ok:
			if err := a.expected(saved.Name.Connector); err != nil {
				return a.notes, err
			}
		case !sameOutput(saved.Name, out):
			if err := a.expected(saved.Name.Connector); err != nil {
				return a.notes, err
			}
			a.note("%s is a different monitor now, so its earlier settings were left alone", out.Name)
		default:
			target[saved.Name.Connector] = restorePolicy(out, saved)
		}
	}
	if err := a.run(ctx, target, before); err != nil {
		return a.notes, err
	}
	return a.notes, nil
}
