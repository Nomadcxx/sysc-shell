package displays

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ticketVersion = 1
	// maxTicketBytes keeps a malformed or hostile ticket from becoming a
	// memory or lock-file problem.
	maxTicketBytes = 1 << 20
	// maxAttempts bounds systemd's Restart=on-failure: a ticket that cannot be
	// resolved after this many starts is left as evidence and reported.
	maxAttempts = 3
)

// Ticket states. The state and the managed file's observed hash are the two
// inputs of the recovery decision table; see decideRecovery.
const (
	StatePreview    = "preview"    // a preview is live, no Keep was confirmed
	StateCommitting = "committing" // Keep was confirmed, publication is not finished
	StateUndo       = "undo"       // a confirmed publication must be undone
	StateKept       = "kept"       // finished, cleanup only
	StateReverted   = "reverted"   // finished, cleanup only
)

var errStale = errors.New("displays: ticket does not describe the running compositor")

// OutputState is what one output looked like before the preview. The embedded
// Policy is the effective policy, not a saved one: a nil field means "not
// honestly knowable" (an unmapped output has no geometry; VRR that is currently
// off does not say whether it is off or merely on demand with nothing to
// refresh), and the UI reports those as unknown rather than guessing.
type OutputState struct {
	Name Name `json:"name"`
	Policy
	// Logical is the effective geometry, kept separately so recovery can
	// fall back to it when an automatic policy no longer yields a usable
	// output.
	Logical *Rect `json:"logical,omitempty"`
}

// Ticket is the durable, private record a transaction is decided from. It is
// written before the first output command and updated at every interruption
// boundary, so both the shell and an independent recovery process reach the
// same conclusion after a crash.
type Ticket struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	State   string `json:"state"`
	Socket  string `json:"socket"`
	Root    string `json:"root"`
	Managed string `json:"managed"`
	// Previous holds the managed file's bytes as of arming, so an undo can
	// restore them without guessing.
	Previous string   `json:"previous"`
	Identity Identity `json:"identity"`
	// DeadlineNS is the armed deadline on CLOCK_BOOTTIME.
	DeadlineNS int64  `json:"deadline_ns"`
	OldHash    string `json:"old_hash"`
	// CandidateHash is the managed file's hash once the confirmed policy has
	// been published.
	CandidateHash string            `json:"candidate_hash,omitempty"`
	Target        map[string]Policy `json:"target,omitempty"`
	Before        []OutputState     `json:"before,omitempty"`
	Attempts      int               `json:"attempts,omitempty"`
	// Note carries a human-readable reason for an unresolved or failed state.
	Note string `json:"note,omitempty"`
}

func (t *Ticket) finished() bool { return t.State == StateKept || t.State == StateReverted }

func (t *Ticket) setState(state, note string) error {
	t.State = state
	t.Note = note
	return t.Store()
}

// NewToken returns the only thing the recovery CLI accepts as input: random hex
// naming a ticket file. No path and no command is ever taken from a caller.
func NewToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("displays: draw a recovery token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func validToken(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// TicketDir is the shell's private runtime directory. It is 0700 and every file
// inside it is 0600, so a ticket cannot be read or replaced by another user.
func TicketDir() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("displays: XDG_RUNTIME_DIR is not set, display transactions need a private runtime directory")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("displays: runtime directory must be absolute")
	}
	dir = filepath.Join(dir, "sysc-displays")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("displays: create ticket directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || stat.Uid != uint32(os.Getuid()) {
		return "", errors.New("displays: recovery directory must be owned by this user and not a symlink")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("displays: secure ticket directory: %w", err)
	}
	return dir, nil
}

func ticketPath(token string) (string, error) {
	if !validToken(token) {
		return "", errors.New("displays: recovery token must be 32 lowercase hex characters")
	}
	dir, err := TicketDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, token+".json"), nil
}

func previousPath(dir, token string) string { return filepath.Join(dir, token+".previous") }
func readyPath(dir, token string) string    { return filepath.Join(dir, token+".ready") }

// LoadTicket reads a ticket and refuses anything structurally unusable.
func LoadTicket(token string) (Ticket, error) {
	var t Ticket
	path, err := ticketPath(token)
	if err != nil {
		return t, err
	}
	data, err := readTicketFile(path)
	if err != nil {
		return t, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Ticket{}, fmt.Errorf("displays: ticket %s is malformed: %w", token, err)
	}
	if t.Version != ticketVersion {
		return Ticket{}, fmt.Errorf("displays: ticket %s has version %d, this shell speaks %d", token, t.Version, ticketVersion)
	}
	switch t.State {
	case StatePreview, StateCommitting, StateUndo, StateKept, StateReverted:
	default:
		return Ticket{}, fmt.Errorf("displays: ticket %s has unknown state %q", token, t.State)
	}
	if t.Socket == "" || t.Root == "" || t.Managed == "" {
		return Ticket{}, fmt.Errorf("displays: ticket %s is missing its compositor paths", token)
	}
	if err := validateTicket(t, token); err != nil {
		return Ticket{}, err
	}
	return t, nil
}

func validateTicket(t Ticket, token string) error {
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	if t.Token != token || !filepath.IsAbs(t.Root) || !filepath.IsAbs(t.Socket) || t.Managed != ManagedPath(t.Root) || t.Previous != previousPath(dir, token) {
		return errors.New("displays: inconsistent recovery ticket paths or token")
	}
	if t.Identity.UID != os.Getuid() || t.Identity.PID <= 0 || t.Identity.StartTicks <= 0 || t.Identity.BootID == "" || t.DeadlineNS <= 0 || t.Attempts < 0 || t.Attempts > maxAttempts {
		return errors.New("displays: invalid recovery identity or deadline")
	}
	if _, err := hex.DecodeString(t.OldHash); err != nil || len(t.OldHash) != 64 {
		return errors.New("displays: invalid original hash")
	}
	if t.CandidateHash != "" {
		if _, err := hex.DecodeString(t.CandidateHash); err != nil || len(t.CandidateHash) != 64 {
			return errors.New("displays: invalid candidate hash")
		}
	}
	if len(t.Before) == 0 || len(t.Before) > 128 || len(t.Target) > 128 {
		return errors.New("displays: invalid output count")
	}
	seen := map[string]bool{}
	for _, state := range t.Before {
		name := state.Name.Connector
		if name == "" || len(name) > 255 || strings.ContainsAny(name, "\x00\n\r") || seen[name] {
			return errors.New("displays: invalid output identity")
		}
		seen[name] = true
		if err := validatePolicy(state.Name, state.Policy); err != nil {
			return err
		}
		if state.Logical != nil {
			if err := validateRect(*state.Logical); err != nil {
				return err
			}
		}
	}
	for name, policy := range t.Target {
		if !seen[name] {
			return errors.New("displays: unknown recovery output")
		}
		if err := validatePolicy(Name{Connector: name}, policy); err != nil {
			return err
		}
		if policy.Relative != nil && (!seen[policy.Relative.To] || name == policy.Relative.To || !slices.Contains([]string{"left", "right", "above", "below"}, policy.Relative.Side)) {
			return errors.New("displays: invalid relative recovery target")
		}
	}
	return nil
}

// Store writes the ticket atomically, so a crash leaves either the previous
// state or the new one, never a half-written record.
func (t *Ticket) Store() error {
	path, err := ticketPath(t.Token)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("displays: encode ticket: %w", err)
	}
	if err := writeFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func readTicketFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("displays: read ticket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("displays: ticket %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("displays: ticket %s is not a regular file", path)
	}
	if info.Size() > maxTicketBytes {
		return nil, fmt.Errorf("displays: ticket %s is larger than %d bytes", path, maxTicketBytes)
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("displays: ticket %s must be private", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("displays: ticket %s has another owner", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, errors.New("displays: ticket changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTicketBytes+1))
	if err != nil {
		return nil, fmt.Errorf("displays: read ticket: %w", err)
	}
	if len(data) > maxTicketBytes {
		return nil, fmt.Errorf("displays: ticket %s is larger than %d bytes", path, maxTicketBytes)
	}
	return data, nil
}

// writePrevious stores the managed file's pre-publication bytes beside the
// ticket so an undo restores exactly what was there.
func writePrevious(dir, token string, data []byte) (string, error) {
	path := previousPath(dir, token)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func readPrevious(path string) ([]byte, error) {
	data, err := readTicketFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// activeTicket reports the unfinished transaction in this ticket directory, so a
// restarted shell joins or reports it instead of starting a second preview.
func activeTicket() (Ticket, bool, error) {
	return activeTicketFor("")
}

func activeTicketFor(socket string) (Ticket, bool, error) {
	dir, err := TicketDir()
	if err != nil {
		return Ticket{}, false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Ticket{}, false, fmt.Errorf("displays: scan tickets: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".json") && len(name) == 37 && validToken(strings.TrimSuffix(name, ".json")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		token := strings.TrimSuffix(name, ".json")
		t, err := LoadTicket(token)
		if err != nil {
			return Ticket{}, false, &unreadableTicket{Token: token, Err: err}
		}
		if !t.finished() && (socket == "" || t.Socket == socket) {
			return t, true, nil
		}
	}
	return Ticket{}, false, nil
}

// unreadableTicket is a record in the ticket directory that does not load. It
// names nothing that can be restored, so it is kept as evidence and the page
// offers to move it aside rather than refusing every preview for good.
type unreadableTicket struct {
	Token string
	Err   error
}

func (e *unreadableTicket) Error() string { return e.Err.Error() }
func (e *unreadableTicket) Unwrap() error { return e.Err }

// setAside moves a record out of the way of later previews without deleting
// it. The files are renamed into a subdirectory the ticket scan ignores, so a
// record that cannot be read, or is not a regular file, moves as well as one
// that can. The journal keeps the helper's own reports past logout.
func setAside(dir, token string) (string, error) {
	to := filepath.Join(dir, "unresolved", token)
	if err := os.MkdirAll(to, 0o700); err != nil {
		return "", fmt.Errorf("displays: create %s: %w", to, err)
	}
	for _, name := range []string{token + ".json", token + ".previous", token + ".ready"} {
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(to, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("displays: set %s aside: %w", name, err)
		}
	}
	return to, cleanupTicket(dir, token)
}

// withTicketLock serializes the durable decision between the shell and the
// recovery helper. Both processes open the same flock file, so a Keep that is
// mid-publication and a recovery that arrives together produce one decision.
func withTicketLock(dir, token string, fn func() error) error {
	return withTicketLockContext(context.Background(), dir, token, fn)
}

func withTicketLockContext(ctx context.Context, dir, token string, fn func() error) error {
	return withNamedLock(ctx, filepath.Join(dir, token+".lock"), fn)
}

func withCompositorLock(ctx context.Context, socket string, fn func() error) error {
	dir, err := TicketDir()
	if err != nil {
		return err
	}
	return withNamedLock(ctx, filepath.Join(dir, "compositor-"+Hash([]byte(socket))+".lock"), fn)
}

func withNamedLock(ctx context.Context, path string, fn func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("displays: open decision lock: %w", err)
	}
	defer f.Close()
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fmt.Errorf("displays: take decision lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:errcheck // released by close too
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func cleanupTicket(dir, token string) error {
	var firstErr error
	for _, name := range []string{token + ".json", token + ".previous", token + ".ready", token + ".lock"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("displays: remove ticket files: %w", firstErr)
	}
	return syncDir(dir)
}
