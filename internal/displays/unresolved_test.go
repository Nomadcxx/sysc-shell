package displays

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshotOf publishes once and returns what the page would render.
func snapshotOf(t *testing.T, svc *Service) Snapshot {
	t.Helper()
	ch := svc.Subscribe()
	svc.Refresh()
	return <-ch
}

// failPreview leaves the record a compositor refusal produces: the apply and
// the shell's own restore both fail, so the ticket stays as evidence.
func failPreview(t *testing.T, svc *Service, fake *fakeNiri) string {
	t.Helper()
	fake.mu.Lock()
	fake.failAfter = 2
	fake.mu.Unlock()
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err == nil {
		t.Fatal("broken preview succeeded")
	}
	active, ok, err := activeTicketFor(svc.socket)
	if err != nil || !ok {
		t.Fatalf("failed restoration left no ticket: ok=%v err=%v", ok, err)
	}
	return active.Token
}

func healFake(fake *fakeNiri) {
	fake.mu.Lock()
	fake.failAfter, fake.queryErr = 0, false
	fake.mu.Unlock()
}

// The live preview is over once its restore failed: the page must report the
// record, not a countdown with Keep and Revert that can never finish.
func TestFailedRestoreIsReportedAsUnresolved(t *testing.T) {
	svc, fake := serviceFor(t)
	token := failPreview(t, svc, fake)
	if svc.ticket != nil {
		t.Fatal("the shell still drives a preview whose restore failed")
	}
	healFake(fake)
	snap := snapshotOf(t, svc)
	if snap.Preview {
		t.Fatal("snapshot still reports a live preview")
	}
	if snap.Unresolved == nil || snap.Unresolved.Token != token || snap.Unresolved.Unreadable {
		t.Fatalf("Unresolved = %+v, want the readable record %s", snap.Unresolved, token)
	}
	if !strings.Contains(snap.Unresolved.Note, "injected") {
		t.Fatalf("Unresolved note = %q, want the restore failure", snap.Unresolved.Note)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(2.0)}}); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("Preview over an unresolved record = %v, want a refusal", err)
	}
}

func TestResolveRestoresAnUnresolvedPreview(t *testing.T) {
	svc, fake := serviceFor(t)
	failPreview(t, svc, fake)
	healFake(fake)
	if err := svc.Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := fake.scale("DP-1"); got != 1 {
		t.Fatalf("scale after Resolve = %v, want the pre-preview 1", got)
	}
	if snap := snapshotOf(t, svc); snap.Unresolved != nil {
		t.Fatalf("Unresolved after Resolve = %+v", snap.Unresolved)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("Preview after Resolve: %v", err)
	}
}

// What Resolve cannot restore it moves out of the way of later previews, so
// the evidence survives and Preview is usable again.
func TestResolveSetsAsideWhatItCannotRestore(t *testing.T) {
	svc, fake := serviceFor(t)
	token := failPreview(t, svc, fake)
	err := svc.Resolve(context.Background())
	aside := filepath.Join(mustTicketDir(), "unresolved", token)
	if err == nil || !strings.Contains(err.Error(), aside) {
		t.Fatalf("Resolve = %v, want the failure and %s", err, aside)
	}
	if _, err := LoadTicket(token); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ticket still in the runtime directory: %v", err)
	}
	for _, name := range []string{token + ".json", token + ".previous"} {
		if _, err := os.Stat(filepath.Join(aside, name)); err != nil {
			t.Fatalf("evidence %s was not kept: %v", name, err)
		}
	}
	// The outputs were left as the failed apply put them, so the baseline
	// check refuses until the native settings are back. That refusal is not
	// the record's: put the baseline back and the record must not block.
	healFake(fake)
	fake.mu.Lock()
	fake.outs["DP-1"].scale = 1
	fake.mu.Unlock()
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("Preview after setting the record aside: %v", err)
	}
}

// sysc-2289: one unreadable record must not refuse every preview with no way
// out from the page.
func TestUnreadableRecordCanBeSetAside(t *testing.T) {
	svc, _ := serviceFor(t)
	token := strings.Repeat("ab", 16)
	if err := os.WriteFile(filepath.Join(mustTicketDir(), token+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := snapshotOf(t, svc)
	if snap.Unresolved == nil || snap.Unresolved.Token != token || !snap.Unresolved.Unreadable {
		t.Fatalf("Unresolved = %+v, want the unreadable record", snap.Unresolved)
	}
	if err := svc.Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mustTicketDir(), "unresolved", token, token+".json")); err != nil {
		t.Fatalf("unreadable record was not kept: %v", err)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("Preview after setting the record aside: %v", err)
	}
}

// A helper that has used its attempts keeps the evidence and exits cleanly:
// a failure exit only makes systemd restart it until the start limit.
func TestRecoveryGivesUpWithoutAskingForARestart(t *testing.T) {
	svc, fake := serviceFor(t)
	token := failPreview(t, svc, fake)
	healFake(fake)
	tk, err := LoadTicket(token)
	if err != nil {
		t.Fatal(err)
	}
	tk.Attempts = maxAttempts
	if err := tk.Store(); err != nil {
		t.Fatal(err)
	}
	if err := RunRecovery(context.Background(), token); err != nil {
		t.Fatalf("a helper out of attempts must exit cleanly: %v", err)
	}
	if _, err := LoadTicket(token); err != nil {
		t.Fatalf("the helper dropped the evidence: %v", err)
	}
}
