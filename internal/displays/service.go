package displays

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// stopRecoveryUnit tells systemd the helper is no longer needed. It is best
// effort: a unit that cannot be stopped still has a ticket that says the
// decision is already made, so the helper's next start does nothing.
var stopRecoveryUnit = func(ctx context.Context, token string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, path, "--user", "stop", "sysc-display-recovery-"+token).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user stop: %v: %s", err, excerpt(string(out)))
	}
	return nil
}

// Output is one live output as the page needs it: what it is, what it can do,
// and the policy the shell would apply to it.
type Output struct {
	Name Name
	// Policy is the effective policy when nothing is drafted, and the previewed
	// policy while a preview is live.
	Policy
	// Logical is the effective geometry, nil while the output is not mapped.
	Logical *Rect
	// Modes are the modes the output advertises.
	Modes []Mode
	// VRRSupported reports whether the hardware can vary refresh at all.
	VRRSupported bool
}

// Snapshot is the whole state the page renders from. It is published by value:
// a subscriber holding one cannot see a later change half-applied.
type Snapshot struct {
	Outputs []Output
	// Preview is true while a live preview is waiting to be kept or reverted.
	Preview bool
	// Remaining is how long the armed window has left, read from the shared
	// boot clock. The page shows it as a countdown; it is zero when nothing is
	// armed. The independent helper reverts when the window closes whether or
	// not the shell is alive to redraw this.
	Remaining time.Duration
	// Note is the message the page shows: the last conflict, refusal or
	// recovery note, or "" when there is nothing to say.
	Note string
}

// Service owns one niri socket and one configuration root. Every command runs
// under one mutex, so a preview, a keep and a revert cannot interleave; the
// mutex is the queue. Nothing here touches Wayland, and nothing here may be
// called while a caller holds the registry lock or stands on the Wayland owner
// goroutine: every command blocks on the compositor.
type Service struct {
	socket   string
	root     string
	validate Validator

	// mu serializes commands.
	mu     sync.Mutex
	own    Owned
	ticket *Ticket
	note   string
	// stopTick ends the one-second republish loop that keeps a countdown
	// moving. It runs only while a preview is armed.
	stopTick context.CancelFunc

	subMu sync.Mutex
	subs  []chan Snapshot
}

// New loads the ownership record for a confirmed root. A root the shell has not
// set up is refused here rather than at the first preview, so the page can say
// why the feature is unavailable before it offers anything.
func New(socket, root string, validate Validator) (*Service, error) {
	if socket == "" {
		return nil, errors.New("displays: no niri socket")
	}
	own, err := LoadOwnership(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSetUp, err)
	}
	if own.Managed == "" {
		return nil, ErrNotSetUp
	}
	return &Service{socket: socket, root: root, own: own, validate: validate}, nil
}

// Root is the configuration root the service was given, after ownership
// resolution. The page shows it for confirmation.
func (s *Service) Root() string { return s.own.Root }

// Subscribe returns a channel that receives a snapshot after every change. The
// channel holds one snapshot: a subscriber that is busy misses the intermediate
// state, not the latest one.
func (s *Service) Subscribe() <-chan Snapshot {
	ch := make(chan Snapshot, 1)
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subs = append(s.subs, ch)
	return ch
}

func (s *Service) publish() {
	// Building the snapshot queries the compositor, so a failing query is
	// reported as a note rather than a panic: the page still needs to render.
	live, err := niri.Outputs(context.Background(), s.socket)
	if err != nil {
		s.note = err.Error()
	}
	target := map[string]Policy{}
	if s.ticket != nil {
		target = s.ticket.Target
	}
	snap := Snapshot{Outputs: s.outputs(live, target), Note: s.note}
	if s.ticket != nil {
		snap.Preview = true
		if left, err := Remaining(s.ticket.DeadlineNS); err == nil {
			snap.Remaining = left
		}
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- snap:
		default:
		}
	}
}

// outputs projects the live query, filling any output the draft does not
// mention with the policy it already has. A preview always covers every output:
// an output the page never showed must not keep a policy the managed file does
// not agree with.
func (s *Service) outputs(live []niri.Output, target map[string]Policy) []Output {
	out := make([]Output, 0, len(live))
	for _, o := range live {
		want, ok := target[o.Name]
		if !ok {
			want, _ = effectivePolicy(o)
		}
		out = append(out, Output{
			Name:         outputName(o),
			Policy:       want,
			Logical:      logicalRect(o),
			Modes:        advertised(o),
			VRRSupported: o.VRRSupported,
		})
	}
	slices.SortFunc(out, func(a, b Output) int { return strings.Compare(a.Name.Connector, b.Name.Connector) })
	return out
}

func logicalRect(o niri.Output) *Rect {
	if o.Logical == nil {
		return nil
	}
	return &Rect{
		Position: Position{X: o.Logical.X, Y: o.Logical.Y},
		Width:    int32(o.Logical.Width),
		Height:   int32(o.Logical.Height),
	}
}

// Refresh re-queries the compositor and publishes. It is the only command that
// changes nothing, and the page calls it whenever it is shown.
func (s *Service) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publish()
}

// Preview applies a draft to the live outputs. It arms recovery first: from the
// moment the first output command is sent, a process that can undo it is
// already running and has said it can reach the compositor.
func (s *Service) Preview(ctx context.Context, draft map[string]Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.validate == nil {
		return ErrNoValidator
	}
	if s.ticket != nil {
		return errors.New("displays: a preview is already live")
	}
	if err := CheckOwnership(s.root, s.own); err != nil {
		return err
	}
	target, before, err := s.plan(ctx, draft)
	if err != nil {
		return err
	}
	candidate, err := s.candidate(target)
	if err != nil {
		return err
	}
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	if err := StageAndValidate(ctx, s.validate, filepath.Join(dir, "validate"), candidate, s.root); err != nil {
		return err
	}
	t, err := arm(ctx, s.socket, s.own, target, before)
	if err != nil {
		return err
	}
	s.ticket = &t
	if err := applyTarget(ctx, s.socket, target, before); err != nil {
		// The preview did not take. Put back what the applier may have
		// already sent, and leave nothing armed.
		_, _ = restore(ctx, s.socket, before)
		s.abandon(&t, err.Error())
		return err
	}
	s.note = ""
	s.startTick()
	s.publish()
	return nil
}

// Keep makes the previewed configuration durable. The intent is written before
// the file is replaced, so a crash in the middle of publication leaves a
// decision to finish rather than a half-written configuration with nobody
// responsible for it.
func (s *Service) Keep(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.ticket
	if t == nil {
		return errors.New("displays: there is no preview to keep")
	}
	if left, err := Remaining(t.DeadlineNS); err != nil {
		return err
	} else if left <= 0 {
		// The window closed, so this is a new decision and the armed recovery
		// owns it. Refusing here does not revoke the preview: the helper will
		// revert it, and pretending otherwise would leave the user unsure
		// whether the change is live.
		return fmt.Errorf("displays: the preview window closed %s ago; the preview is being reverted", -left)
	}
	if err := CheckOwnership(s.root, s.own); err != nil {
		return err
	}
	candidate, err := s.candidate(t.Target)
	if err != nil {
		return err
	}
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	if err := StageAndValidate(ctx, s.validate, filepath.Join(dir, "validate"), candidate, s.root); err != nil {
		return err
	}
	t.CandidateHash = Hash(candidate)
	if err := t.setState(StateCommitting, "the configuration was confirmed"); err != nil {
		return err
	}
	if err := PublishManaged(&s.own, candidate); err != nil {
		return err
	}
	if err := niri.Action(ctx, s.socket, niri.LoadConfigFile{Path: s.root}); err != nil {
		return err
	}
	if err := verifyTarget(ctx, s.socket, t.Target); err != nil {
		return err
	}
	s.ticket = nil
	s.note = "Saved to the niri configuration."
	s.settle(t, StateKept)
	s.publish()
	return nil
}

// Revert gives the outputs back what they had before the preview.
func (s *Service) Revert(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.ticket
	if t == nil {
		return nil
	}
	notes, err := restore(ctx, s.socket, t.Before)
	if err != nil {
		s.note = err.Error()
		s.publish()
		return err
	}
	s.ticket = nil
	s.note = joinNotes(notes)
	s.settle(t, StateReverted)
	s.publish()
	return nil
}

// Close reverts a live preview and drops the subscribers. The service is
// unusable afterwards, which is what closing a page means.
func (s *Service) Close(ctx context.Context) error {
	err := s.Revert(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subs = nil
	return err
}

// abandon leaves a ticket the service has decided to stop caring about. Nothing
// armed may outlive the decision, so the helper is stopped and its files go;
// the reason is the note the page shows.
func (s *Service) abandon(t *Ticket, note string) {
	s.note = note
	t.Note = note
	_ = t.Store()
	s.settle(t, StateReverted)
}

// startTick republishes once a second while a preview is armed, so the
// countdown on the page moves. It is the only periodic work the service does:
// with nothing armed there is no timer and no redraw.
func (s *Service) startTick() {
	if s.stopTick != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stopTick = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				armed := s.ticket != nil
				if armed {
					s.publish()
				}
				s.mu.Unlock()
				if !armed {
					return
				}
			}
		}
	}()
}

func (s *Service) stopTicking() {
	if s.stopTick != nil {
		s.stopTick()
		s.stopTick = nil
	}
}

// settle writes a terminal state, stops the helper and removes the ticket. It
// runs under the command mutex, so the helper's own decision sees either the
// state before or the state after, never neither.
func (s *Service) settle(t *Ticket, state string) {
	s.stopTicking()
	if err := t.setState(state, t.Note); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = stopRecoveryUnit(ctx, t.Token)
	dir, err := TicketDir()
	if err != nil {
		return
	}
	_ = cleanupTicket(dir, t.Token)
}

// plan turns a draft into a target that covers every live output, and captures
// the state a restore would need. Validation here is the shell's own rules; the
// only authority on whether niri accepts the result is the staged validation
// that follows.
func (s *Service) plan(ctx context.Context, draft map[string]Policy) (map[string]Policy, []OutputState, error) {
	live, err := niri.Outputs(ctx, s.socket)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(live, func(a, b niri.Output) int { return strings.Compare(a.Name, b.Name) })
	target := make(map[string]Policy, len(live))
	rects := make(map[string]Rect, len(live))
	before := make([]OutputState, 0, len(live))
	connected := make(map[string]bool, len(live))
	for _, o := range live {
		connected[o.Name] = true
		want, ok := draft[o.Name]
		if !ok {
			if want, _ = effectivePolicy(o); !ok {
				want = Policy{}
			}
		}
		if _, _, err := planOutput(o, want); err != nil {
			return nil, nil, err
		}
		if want.Scale != nil {
			if err := ValidateScale(*want.Scale); err != nil {
				return nil, nil, fmt.Errorf("displays: %s: %w", o.Name, err)
			}
		}
		if want.Mode != nil {
			if err := ValidateMode(advertised(o), *want.Mode); err != nil {
				return nil, nil, fmt.Errorf("displays: %s: %w", o.Name, err)
			}
		}
		if want.Transform != nil {
			if err := ParseTransformNative(*want.Transform); err != nil {
				return nil, nil, fmt.Errorf("displays: %s: %w", o.Name, err)
			}
		}
		if want.Position != nil && o.Logical != nil {
			rects[o.Name] = Rect{
				Position: *want.Position,
				Width:    int32(o.Logical.Width),
				Height:   int32(o.Logical.Height),
			}
		}
		target[o.Name] = want
		before = append(before, beforeState(o))
	}
	// A draft naming an output that is not connected is refused rather than
	// dropped. Silently applying the rest would leave the managed file and the
	// live session disagreeing about an output the user just set.
	for name := range draft {
		if !connected[name] {
			return nil, nil, fmt.Errorf("displays: %s is not connected", name)
		}
	}
	if err := ValidateArrangement(rects); err != nil {
		return nil, nil, err
	}
	return target, before, nil
}

// candidate renders the owned file for a target. The order is the target's own
// keys, so a Keep recomputes byte for byte what the preview validated.
func (s *Service) candidate(target map[string]Policy) ([]byte, error) {
	graph, err := LoadGraph(s.root)
	if err != nil {
		return nil, err
	}
	order := make([]string, 0, len(target))
	for name := range target {
		order = append(order, name)
	}
	sort.Strings(order)
	return BuildManaged(graph, order, target)
}
