package displays

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// Recovery runs in its own process, started before the preview touches any
// output, and never opens Wayland: it only talks to niri's socket and to the
// files it owns. A shell that crashes, is killed or loses power leaves the
// ticket behind, and this process is the only thing that can undo it.

// startRecoveryUnit is the seam tests replace; production runs systemd-run.
var startRecoveryUnit = func(ctx context.Context, exe, token string) error {
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return fmt.Errorf("displays: find systemd-run: %w", err)
	}
	// Restart=on-failure covers a helper that dies before it decides, for
	// example because the compositor was still starting. The unit is bounded
	// by systemd's own start limit and by maxAttempts below.
	argv := []string{
		"--user", "--collect",
		"--unit=sysc-display-recovery-" + token,
		"--property=Restart=on-failure",
		"--property=RestartSec=1s",
		exe, "--restore-displays", token,
	}
	out, err := exec.CommandContext(ctx, path, argv...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("displays: start recovery: %v: %s", err, excerpt(string(out)))
	}
	return nil
}

// arm starts the recovery process for a preview and returns the ticket it will
// act on. It is called before the first output command, so from the moment any
// output changes there is a process alive that knows how to put it back.
func arm(ctx context.Context, socket string, own Owned, target map[string]Policy, before []OutputState) (Ticket, error) {
	dir, err := TicketDir()
	if err != nil {
		return Ticket{}, err
	}
	token, err := NewToken()
	if err != nil {
		return Ticket{}, err
	}
	identity, err := compositorIdentity(socket)
	if err != nil {
		return Ticket{}, err
	}
	managed, err := os.ReadFile(own.Managed)
	if err != nil {
		return Ticket{}, fmt.Errorf("displays: read %s: %w", own.Managed, err)
	}
	now, err := bootNow()
	if err != nil {
		return Ticket{}, err
	}
	previous, err := writePrevious(dir, token, managed)
	if err != nil {
		return Ticket{}, err
	}
	t := Ticket{
		Version:  ticketVersion,
		Token:    token,
		State:    StatePreview,
		Socket:   socket,
		Root:     own.Root,
		Managed:  own.Managed,
		Previous: previous,
		Identity: identity,
		// bootNow is already a duration since boot, so the deadline is the sum
		// in nanoseconds: the same unit the timerfd wait takes.
		DeadlineNS: int64(now + armWindow),
		OldHash:    Hash(managed),
		Target:     target,
		Before:     before,
	}
	if err := t.Store(); err != nil {
		return Ticket{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Ticket{}, fmt.Errorf("displays: locate this executable: %w", err)
	}
	if err := startRecoveryUnit(ctx, exe, token); err != nil {
		return Ticket{}, err
	}
	if err := waitReady(ctx, dir, t); err != nil {
		return Ticket{}, err
	}
	return t, nil
}

// waitReady blocks until the recovery process has published its marker, bounded
// by the same boot-clock deadline the ticket carries. systemd-run's own exit
// says only that the unit was accepted, so it is not the wait: the marker is
// the process asserting it can still see the compositor this ticket describes.
func waitReady(ctx context.Context, dir string, t Ticket) error {
	marker := readyPath(dir, t.Token)
	for {
		if _, err := os.Stat(marker); err == nil {
			return nil
		}
		left, err := Remaining(t.DeadlineNS)
		if err != nil {
			return err
		}
		if left <= 0 {
			return fmt.Errorf("displays: recovery did not report ready within %s", armWindow)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(left, 100*time.Millisecond)):
		}
	}
}

// RunRecovery is the helper's entire job, and the only entry point its CLI
// accepts. It is safe to run twice, safe to run against a ticket that is
// already resolved, and safe to interrupt at any point: every state change is
// written durably before the work it authorizes.
func RunRecovery(ctx context.Context, token string) error {
	if !validToken(token) {
		return fmt.Errorf("displays: %q is not a recovery token", token)
	}
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	t, err := LoadTicket(token)
	if err != nil {
		return err
	}
	// A ticket names one compositor process. If the peer is a different one,
	// the preview it describes is gone and acting on it would change somebody
	// else's displays.
	now, err := compositorIdentity(t.Socket)
	if err != nil {
		return err
	}
	if !now.Equal(t.Identity) {
		return fmt.Errorf("%w: ticket %s expected %s, peer is %s", errStale, token, t.Identity, now)
	}
	if t.Attempts >= maxAttempts {
		return fmt.Errorf("displays: recovery %s has run %d times without resolving: %s", token, t.Attempts, t.Note)
	}
	t.Attempts++
	if err := t.Store(); err != nil {
		return err
	}
	if err := writeFileAtomic(readyPath(dir, token), []byte(now.String()+"\n"), 0o600); err != nil {
		return err
	}
	if err := sleepUntilBoot(t.DeadlineNS); err != nil {
		return err
	}
	return withTicketLock(dir, token, func() error {
		// Re-read under the lock: the shell may have finished a Keep while this
		// process was waiting for its deadline.
		fresh, err := LoadTicket(token)
		if err != nil {
			return err
		}
		return recoverLocked(ctx, dir, &fresh)
	})
}

func recoverLocked(ctx context.Context, dir string, t *Ticket) error {
	observed := fileDigest(t.Managed)
	action := decideRecovery(t.State, t.OldHash, t.CandidateHash, observed)
	switch action {
	case actionConflict:
		// The owned file holds bytes the shell did not write. Leaving them is
		// the only safe answer; the runtime is still restored so the session
		// is usable, and the conflict is reported rather than resolved.
		notes, err := restore(ctx, t.Socket, t.Before)
		return finish(dir, t, StateReverted, joinNotes(append(notes,
			fmt.Sprintf("%s holds content this ticket did not write; it was left alone", t.Managed))), err)
	case actionRestoreRuntime:
		notes, err := restore(ctx, t.Socket, t.Before)
		return finish(dir, t, StateReverted, joinNotes(notes), err)
	case actionFinishKeep:
		if err := finishKeep(ctx, t); err != nil {
			return fail(t, err)
		}
		return finish(dir, t, StateKept, "the confirmed configuration was reloaded and verified", nil)
	case actionUndoPublish:
		if err := undoPublish(t); err != nil {
			return fail(t, err)
		}
		notes, err := restore(ctx, t.Socket, t.Before)
		return finish(dir, t, StateReverted, joinNotes(notes), err)
	case actionNone:
		return cleanupTicket(dir, t.Token)
	}
	return fmt.Errorf("displays: unreachable recovery action %d", action)
}

// finish records a terminal state and removes the ticket. An error is reported
// but still recorded, so a reader can see that the decision was made even when
// the compositor refused a command.
func finish(dir string, t *Ticket, state, note string, err error) error {
	if serr := t.setState(state, note); serr != nil {
		return serr
	}
	report(state, note, err)
	if err != nil {
		return err
	}
	return cleanupTicket(dir, t.Token)
}

// fail leaves the ticket in place. A helper that cannot resolve its problem
// must not delete the evidence of it.
func fail(t *Ticket, err error) error {
	t.Note = err.Error()
	if serr := t.Store(); serr != nil {
		return serr
	}
	report("unresolved", t.Note, err)
	return err
}

// report writes the outcome to stderr, which is the journal for a unit started
// by systemd-run. It is the only place a resolved conflict stays visible.
func report(state, note string, err error) {
	// A unit that ran and said nothing is indistinguishable from a unit that
	// never started, so the terminal state is always reported and the detail is
	// added when there is any.
	detail := strings.TrimSpace(joinNotes([]string{note, errText(err)}))
	if detail == "" {
		fmt.Fprintf(os.Stderr, "sysc-shell display recovery: %s\n", state)
		return
	}
	fmt.Fprintf(os.Stderr, "sysc-shell display recovery: %s: %s\n", state, detail)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func joinNotes(notes []string) string {
	kept := notes[:0]
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, "; ")
}

// finishKeep completes a confirmed Keep whose candidate bytes are on disk: the
// compositor is told to re-read the root it was configured with, and the result
// is read back rather than assumed.
func finishKeep(ctx context.Context, t *Ticket) error {
	if err := niri.Action(ctx, t.Socket, niri.LoadConfigFile{Path: t.Root}); err != nil {
		return fmt.Errorf("displays: reload %s: %w", t.Root, err)
	}
	return verifyTarget(ctx, t.Socket, t.Target)
}

// undoPublish puts the managed file back to the bytes it held when the preview
// was armed, but only while it still holds the candidate: a file that changed
// again in the meantime is somebody's work, not an interrupted publication.
func undoPublish(t *Ticket) error {
	if fileDigest(t.Managed) != t.CandidateHash {
		return fmt.Errorf("%w: %s changed again after the confirmed publication", ErrExternalEdit, t.Managed)
	}
	previous, err := readPrevious(t.Previous)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(t.Managed, previous, 0o600); err != nil {
		return err
	}
	// The owned bytes are ours again, so the ownership record's baseline has to
	// follow them; otherwise the next check would call our own restoration an
	// external edit.
	own, err := LoadOwnership(t.Root)
	if err != nil {
		return err
	}
	own.ManagedSHA256 = Hash(previous)
	if err := own.Store(t.Root); err != nil {
		return err
	}
	if err := niri.Action(context.Background(), t.Socket, niri.LoadConfigFile{Path: t.Root}); err != nil {
		return fmt.Errorf("displays: reload %s: %w", t.Root, err)
	}
	return nil
}

// verifyTarget reads the compositor back after a reload. An output that is not
// connected is a note, not a failure: nothing a reload can do brings it back.
func verifyTarget(ctx context.Context, socket string, target map[string]Policy) error {
	a := &applier{socket: socket}
	live, err := a.live(ctx)
	if err != nil {
		return err
	}
	var failed []string
	for _, name := range sortedOutputs(live) {
		want, ok := target[name.Name]
		if !ok {
			continue
		}
		if err := verifyOutput(live[name.Name], want); err != nil {
			if err := a.expected(name.Name); err != nil {
				continue
			}
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

type recoveryAction int

const (
	// actionNone is a ticket that is already resolved.
	actionNone recoveryAction = iota
	// actionRestoreRuntime puts the pre-preview state back and leaves the
	// owned file alone.
	actionRestoreRuntime
	// actionFinishKeep reloads the root and verifies, because a confirmed
	// candidate is already on disk.
	actionFinishKeep
	// actionUndoPublish puts the previous owned bytes back first, because a
	// confirmed publication is live in the compositor right now.
	actionUndoPublish
	// actionConflict means the owned file is not what this ticket wrote. It is
	// preserved and reported.
	actionConflict
)

// decideRecovery is the durable decision table: the ticket's state crossed with
// what the owned file actually holds. It is a pure function so the whole table
// is one test rather than one test per shell command.
func decideRecovery(state, oldHash, candidateHash, observed string) recoveryAction {
	if state == StateKept || state == StateReverted {
		return actionNone
	}
	// A file that cannot be read has no bytes to protect, so recovery puts the
	// runtime back and says nothing: the next check then reports the missing
	// file, which the user resolves by adopting. Bytes that are neither ours
	// are somebody's work and are never overwritten.
	known := observed == "" || observed == oldHash || observed == candidateHash
	if !known {
		return actionConflict
	}
	candidatePresent := observed == candidateHash && candidateHash != ""
	switch state {
	case StateUndo:
		if candidatePresent {
			return actionUndoPublish
		}
		return actionRestoreRuntime
	case StateCommitting:
		if candidatePresent {
			return actionFinishKeep
		}
		return actionRestoreRuntime
	default: // StatePreview: no Keep was ever confirmed, so any candidate is a conflict.
		return actionRestoreRuntime
	}
}
