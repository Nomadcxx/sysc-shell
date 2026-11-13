package displays

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	VRREnabled   bool
	Unavailable  string
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
	svc := &Service{socket: socket, root: root, own: own, validate: validate}
	// A shell restarted mid-preview does not take the transaction over: its
	// independent helper still owns it. The page says so, and Preview stays
	// refused until the helper settles it.
	if active, ok, err := activeTicketFor(socket); err != nil {
		svc.note = err.Error()
	} else if ok {
		svc.note = fmt.Sprintf("A display preview from before the shell restarted is still being resolved (%s).", active.State)
	}
	return svc, nil
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
	// The helper can finish after timeout while the shell remains alive.
	if s.ticket != nil {
		if fresh, err := LoadTicket(s.ticket.Token); errors.Is(err, os.ErrNotExist) || (err == nil && fresh.finished()) {
			s.ticket = nil
			s.stopTicking()
			s.note = "The display preview ended."
			if own, err := LoadOwnership(s.root); err == nil {
				s.own = own
			}
		}
	}
	// Building the snapshot queries the compositor, so a failing query is
	// reported as a note rather than a panic: the page still needs to render.
	ctx, cancel := context.WithTimeout(context.Background(), applyBudget)
	defer cancel()
	live, err := niri.Outputs(ctx, s.socket)
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
		case <-ch:
		default:
		}
		ch <- snap
	}
}

// outputs projects the live query, filling any output the draft does not
// mention with the policy it already has. A preview always covers every output:
// an output the page never showed must not keep a policy the managed file does
// not agree with.
func (s *Service) outputs(live []niri.Output, target map[string]Policy) []Output {
	graph, graphErr := LoadGraph(s.root)
	out := make([]Output, 0, len(live))
	for _, o := range live {
		var want Policy
		unavailable := ""
		if graphErr != nil {
			unavailable = graphErr.Error()
		} else {
			var err error
			want, unavailable, err = savedPolicy(graph, outputName(o))
			if err != nil {
				unavailable = err.Error()
			}
		}
		if o.Logical == nil {
			unavailable = "This display is disabled; enable it in the native configuration before editing it."
		}
		if o.IsCustomMode {
			unavailable = "Custom display modes cannot be edited here."
		}
		if preview, ok := target[o.Name]; ok {
			want = preview
		}
		out = append(out, Output{Name: outputName(o), Policy: clonePolicy(want), Logical: logicalRect(o), Modes: advertised(o), VRRSupported: o.VRRSupported, VRREnabled: o.VRREnabled, Unavailable: unavailable})
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
	return withCompositorLock(ctx, s.socket, func() error {
		if s.ticket != nil {
			return errors.New("displays: a preview is already live")
		}
		if active, ok, err := activeTicketFor(s.socket); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("displays: preview %s is still unresolved", active.Token)
		}
		if err := CheckOwnership(s.root, s.own); err != nil {
			return err
		}
		target, before, err := s.plan(ctx, draft)
		if err != nil {
			return err
		}
		candidate, err := s.candidate(target, before)
		if err != nil {
			return err
		}
		dir, err := TicketDir()
		if err != nil {
			return err
		}
		if err := StageAndValidate(ctx, s.validate, filepath.Join(dir, "validate-"+Hash([]byte(s.socket))[:16]), candidate, s.root); err != nil {
			return err
		}
		if err := CheckOwnership(s.root, s.own); err != nil {
			return err
		}
		if err := verifyBaseline(ctx, s.socket, before); err != nil {
			return err
		}
		t, err := arm(ctx, s.socket, s.own, target, before)
		if err != nil {
			return err
		}
		s.ticket = &t
		err = withTicketLockContext(ctx, dir, t.Token, func() error {
			if err := checkTicketPeer(t); err != nil {
				return err
			}
			if err := applyTarget(ctx, s.socket, target, before); err != nil {
				return err
			}
			t.Target = target // relative positions were resolved after geometry
			return t.Store()
		})
		if err != nil {
			// A failed/cancelled caller cannot revoke independent recovery. Try
			// restoration with its own budget and keep evidence on any failure.
			restoreCtx, cancel := context.WithTimeout(context.Background(), applyBudget)
			defer cancel()
			err = errors.Join(err, withTicketLockContext(restoreCtx, dir, t.Token, func() error {
				if err := checkTicketPeer(t); err != nil {
					return err
				}
				_, restoreErr := restore(restoreCtx, s.socket, before)
				if restoreErr != nil {
					return fail(&t, restoreErr)
				}
				if settleErr := s.settle(&t, StateReverted); settleErr != nil {
					return settleErr
				}
				s.ticket = nil
				return nil
			}))
			s.note = err.Error()
			return err
		}
		s.note = ""
		s.startTick()
		s.publish()
		return nil
	})
}

// Keep makes the previewed configuration durable. The intent is written before
// the file is replaced, so a crash in the middle of publication leaves a
// decision to finish rather than a half-written configuration with nobody
// responsible for it.
func (s *Service) Keep(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ticket == nil {
		return errors.New("displays: there is no preview to keep")
	}
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	token := s.ticket.Token
	// Native validation can take seconds, so it runs before the decision lock:
	// holding the lock through it would delay the helper past the deadline.
	// Only this command mutex orders the shell's own writes to the ticket.
	staged, err := LoadTicket(token)
	if err != nil {
		return err
	}
	if err := checkKeep(ctx, staged); err != nil {
		return err
	}
	if err := CheckOwnership(s.root, s.own); err != nil {
		return err
	}
	validated, err := s.candidate(staged.Target, staged.Before)
	if err != nil {
		return err
	}
	if err := StageAndValidate(ctx, s.validate, filepath.Join(dir, "validate-"+token), validated, s.root); err != nil {
		return err
	}
	return withTicketLockContext(ctx, dir, token, func() error {
		t, err := LoadTicket(token)
		if err != nil {
			return err
		}
		if t.State != StatePreview {
			return errors.New("displays: preview is being resolved")
		}
		// The final critical section rechecks the deadline, identities and
		// hashes. The validated bytes are published as they are: the KDL
		// generator orders properties nondeterministically, so a second
		// render of the same inputs is not byte-identical. What must not have
		// changed is what those bytes were built from.
		if err := checkKeep(ctx, t); err != nil {
			return err
		}
		if err := CheckOwnership(s.root, s.own); err != nil {
			return err
		}
		if !reflect.DeepEqual(t.Target, staged.Target) || !reflect.DeepEqual(t.Before, staged.Before) {
			return errors.New("displays: the preview changed while it was validated")
		}
		candidate := validated
		t.CandidateHash = Hash(candidate)
		if err := t.setState(StateCommitting, "the configuration was confirmed"); err != nil {
			return err
		}
		s.ticket = &t
		if err := PublishManaged(&s.own, candidate); err != nil {
			return s.undoKeep(&t, err)
		}
		if err := finishKeep(ctx, &t); err != nil {
			return s.undoKeep(&t, err)
		}
		if own, err := LoadOwnership(s.root); err != nil {
			return s.undoKeep(&t, err)
		} else {
			s.own = own
		}
		if err := s.settle(&t, StateKept); err != nil {
			return err
		}
		s.ticket = nil
		s.note = "Saved to the native compositor configuration."
		s.publish()
		return nil
	})
}

func checkKeep(ctx context.Context, t Ticket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkTicketPeer(t); err != nil {
		return err
	}
	fresh, err := LoadTicket(t.Token)
	if err != nil {
		return err
	}
	if fresh.State != StatePreview {
		return errors.New("displays: preview was already resolved")
	}
	if left, err := Remaining(fresh.DeadlineNS); err != nil {
		return err
	} else if left <= 0 {
		return errors.New("displays: preview expired")
	}
	live, err := niri.Outputs(ctx, t.Socket)
	if err != nil {
		return err
	}
	byName := make(map[string]niri.Output, len(live))
	for _, out := range live {
		byName[out.Name] = out
	}
	if err := (&applier{strict: true}).checkIdentities(byName, t.Before); err != nil {
		return err
	}
	return verifyTarget(ctx, t.Socket, t.Target)
}

// undoKeep is called under the same decision lock. Persisting undo before any
// restoration makes the helper resume the correct operation after a crash.
func (s *Service) undoKeep(t *Ticket, cause error) error {
	if err := t.setState(StateUndo, cause.Error()); err != nil {
		return errors.Join(cause, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), applyBudget)
	defer cancel()
	if err := checkTicketPeer(*t); err != nil {
		return errors.Join(cause, fail(t, err))
	}
	if err := recoverLocked(ctx, mustTicketDir(), t); err != nil {
		return errors.Join(cause, err)
	}
	if own, err := LoadOwnership(s.root); err == nil {
		s.own = own
	}
	s.ticket = nil
	s.stopTicking()
	s.note = cause.Error()
	return cause
}

func mustTicketDir() string { dir, _ := TicketDir(); return dir }

// Revert gives the outputs back what they had before the preview.
func (s *Service) Revert(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ticket == nil {
		return nil
	}
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	return withTicketLockContext(ctx, dir, s.ticket.Token, func() error {
		t, err := LoadTicket(s.ticket.Token)
		if errors.Is(err, os.ErrNotExist) {
			s.ticket = nil
			s.stopTicking()
			return nil
		}
		if err != nil {
			return err
		}
		if err := checkTicketPeer(t); err != nil {
			return err
		}
		if t.State == StateCommitting || t.State == StateUndo {
			if err := t.setState(StateUndo, "revert requested"); err != nil {
				return err
			}
			if err := recoverLocked(ctx, dir, &t); err != nil {
				return err
			}
		} else if !t.finished() {
			notes, err := restore(ctx, s.socket, t.Before)
			if err != nil {
				s.note = err.Error()
				return fail(&t, err)
			}
			t.Note = joinNotes(notes)
			if err := s.settle(&t, StateReverted); err != nil {
				return err
			}
		}
		s.ticket = nil
		s.stopTicking()
		s.note = t.Note
		if own, err := LoadOwnership(s.root); err == nil {
			s.own = own
		}
		s.publish()
		return nil
	})
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
func (s *Service) settle(t *Ticket, state string) error {
	if err := t.setState(state, t.Note); err != nil {
		return err
	}
	s.stopTicking()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = stopRecoveryUnit(ctx, t.Token)
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	return cleanupTicket(dir, t.Token)
}

// plan turns a draft into a target that covers every live output, and captures
// the state a restore would need. Validation here is the shell's own rules; the
// only authority on whether niri accepts the result is the staged validation
// that follows.
func (s *Service) plan(ctx context.Context, draft map[string]Policy) (map[string]Policy, []OutputState, error) {
	graph, err := LoadGraph(s.root)
	if err != nil {
		return nil, nil, err
	}
	live, err := niri.Outputs(ctx, s.socket)
	if err != nil {
		return nil, nil, err
	}
	target := map[string]Policy{}
	before := make([]OutputState, 0, len(live))
	connected := map[string]bool{}
	for _, out := range live {
		connected[out.Name] = true
		baseline, unavailable, err := savedPolicy(graph, outputName(out))
		if err != nil {
			return nil, nil, err
		}
		saved := beforeState(out)
		saved.Policy = clonePolicy(baseline)
		before = append(before, saved)
		want, edited := draft[out.Name]
		if !edited || want.Equal(baseline) {
			continue
		}
		if unavailable != "" {
			return nil, nil, errors.New(unavailable)
		}
		if out.Logical == nil || out.IsCustomMode {
			return nil, nil, fmt.Errorf("displays: %s is disabled or running a custom mode", out.Name)
		}
		if err := verifyOutput(out, baseline); err != nil {
			return nil, nil, fmt.Errorf("displays: %s differs from saved policy; restore the native baseline first: %w", out.Name, err)
		}
		if err := validatePolicy(outputName(out), want); err != nil {
			return nil, nil, err
		}
		if want.Mode != nil {
			if err := ValidateMode(advertised(out), *want.Mode); err != nil {
				return nil, nil, err
			}
		}
		if _, _, err := planOutput(out, want); err != nil {
			return nil, nil, err
		}
		if want.Relative != nil {
			if want.Position != nil || want.Relative.To == out.Name {
				return nil, nil, errors.New("displays: invalid relative placement")
			}
			if !slices.Contains([]string{"left", "right", "above", "below"}, want.Relative.Side) {
				return nil, nil, errors.New("displays: invalid relative direction")
			}
			if anchor, ok := draft[want.Relative.To]; ok && anchor.Relative != nil {
				return nil, nil, errors.New("displays: a display cannot be placed beside one that is itself placed relatively")
			}
		}
		target[out.Name] = clonePolicy(want)
	}
	for name := range draft {
		if !connected[name] {
			return nil, nil, fmt.Errorf("displays: %s is not connected", name)
		}
	}
	for _, want := range target {
		if want.Relative != nil && !connected[want.Relative.To] {
			return nil, nil, errors.New("displays: relative display is not connected")
		}
	}
	if len(target) == 0 {
		return nil, nil, errors.New("displays: there are no changed display settings")
	}
	return target, before, nil
}

func verifyBaseline(ctx context.Context, socket string, before []OutputState) error {
	live, err := niri.Outputs(ctx, socket)
	if err != nil {
		return err
	}
	byName := make(map[string]niri.Output, len(live))
	for _, out := range live {
		byName[out.Name] = out
	}
	if err := (&applier{strict: true}).checkIdentities(byName, before); err != nil {
		return err
	}
	for _, saved := range before {
		out := byName[saved.Name.Connector]
		if saved.Logical == nil || out.IsCustomMode {
			continue
		}
		if err := verifyOutput(out, saved.Policy); err != nil {
			return fmt.Errorf("displays: saved baseline changed: %w", err)
		}
	}
	return nil
}

// candidate renders the owned file for a target, outputs sorted by connector.
// Property order inside a node is not stable across renders, which is why Keep
// publishes the bytes it validated rather than rendering again.
func (s *Service) candidate(target map[string]Policy, before []OutputState) ([]byte, error) {
	graph, err := LoadGraph(s.root)
	if err != nil {
		return nil, err
	}
	order := make([]Name, 0, len(target))
	for name := range target {
		saved, ok := findSaved(before, name)
		if !ok {
			return nil, fmt.Errorf("displays: no identity for %s", name)
		}
		order = append(order, saved.Name)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].Connector < order[j].Connector })
	return buildManaged(graph, order, target)
}

func clonePolicy(p Policy) Policy {
	if p.Mode != nil {
		v := *p.Mode
		p.Mode = &v
	}
	if p.Scale != nil {
		v := *p.Scale
		p.Scale = &v
	}
	if p.Transform != nil {
		v := *p.Transform
		p.Transform = &v
	}
	if p.Position != nil {
		v := *p.Position
		p.Position = &v
	}
	if p.Relative != nil {
		v := *p.Relative
		p.Relative = &v
	}
	if p.VRR != nil {
		v := *p.VRR
		p.VRR = &v
	}
	return p
}
