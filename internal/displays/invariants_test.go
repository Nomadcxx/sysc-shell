package displays

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func TestAlwaysOnVRRReadback(t *testing.T) {
	if err := verifyOutput(niri.Output{Name: "DP-1", VRRSupported: true, VRREnabled: true}, Policy{VRR: &VRRPolicy{}}); err != nil {
		t.Fatal(err)
	}
	if err := verifyOutput(niri.Output{Name: "DP-1", VRRSupported: true}, Policy{VRR: &VRRPolicy{}}); err == nil {
		t.Fatal("disabled VRR satisfied always-on policy")
	}
}

func TestGeneratedTransformsUseNativeVocabulary(t *testing.T) {
	root, _ := setupDir(t, "")
	g, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range Transforms {
		t.Run(tx, func(t *testing.T) {
			body, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{"DP-1": {Transform: &tx}})
			if err != nil {
				t.Fatal(err)
			}
			if err := StageAndValidate(context.Background(), niriValidator(t), t.TempDir(), body, root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSnapshotReadsSavedAutomaticPolicy(t *testing.T) {
	svc, _ := serviceFor(t)
	rootData := "output \"DP-1\" {\n variable-refresh-rate on-demand=true\n}\n"
	// Set up a separate configuration so this is not an unadopted external edit.
	root, _ := setupDir(t, rootData)
	if _, err := Setup(root); err != nil {
		t.Fatal(err)
	}
	svc, err := New(svc.socket, root, niriValidator(t))
	if err != nil {
		t.Fatal(err)
	}
	ch := svc.Subscribe()
	svc.Refresh()
	out := (<-ch).Outputs[0]
	if out.Mode != nil || out.Scale != nil || out.Position != nil || out.Transform != nil || out.VRR == nil || !out.VRR.OnDemand {
		t.Fatalf("snapshot substituted observed values for saved policy: %+v", out.Policy)
	}
}

func TestKeepPersistsOwnershipAcrossRestart(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Keep(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(svc.socket, svc.root, svc.validate)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnership(restarted.root, restarted.own); err != nil {
		t.Fatal(err)
	}
}

func TestKeepRejectsExpiredAfterValidation(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	old := readFile(t, svc.own.Managed)
	svc.validate = func(context.Context, string) error {
		now, err := bootNow()
		if err != nil {
			return err
		}
		svc.ticket.DeadlineNS = int64(now - time.Second)
		return svc.ticket.Store()
	}
	if err := svc.Keep(context.Background()); err == nil {
		t.Fatal("Keep committed after expiry")
	}
	if got := readFile(t, svc.own.Managed); got != old {
		t.Fatal("expired Keep rewrote native configuration")
	}
}

func TestKeepRejectsUnpluggedOutput(t *testing.T) {
	svc, fake := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	delete(fake.outs, "DP-1")
	fake.mu.Unlock()
	if err := svc.Keep(context.Background()); err == nil {
		t.Fatal("Keep accepted missing target")
	}
}

func TestKeepFailureRestoresPublishedBytes(t *testing.T) {
	svc, fake := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	old := readFile(t, svc.own.Managed)
	fake.mu.Lock()
	fake.reloadErr = true
	fake.mu.Unlock()
	if err := svc.Keep(context.Background()); err == nil {
		t.Fatal("Keep on a cancelled context succeeded")
	}
	if got := readFile(t, svc.own.Managed); got != old {
		t.Fatal("failed Keep retained candidate bytes")
	}
}

func TestMissingTargetFailsReadback(t *testing.T) {
	svc, fake := serviceFor(t)
	fake.mu.Lock()
	delete(fake.outs, "DP-1")
	fake.mu.Unlock()
	if err := verifyTarget(context.Background(), svc.socket, map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err == nil {
		t.Fatal("readback accepted a missing target")
	}
}

func TestDisconnectedManagedOutputSurvivesAnotherKeep(t *testing.T) {
	svc, _ := serviceFor(t)
	disconnected := []byte("output \"HDMI-A-1\" {\n scale 2\n layout { gaps 9; }\n}\n")
	if err := PublishManaged(&svc.own, disconnected); err != nil {
		t.Fatal(err)
	}
	if err := svc.own.Store(svc.root); err != nil {
		t.Fatal(err)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Keep(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, svc.own.Managed)
	if !strings.Contains(body, "HDMI-A-1") || !strings.Contains(body, "gaps 9") {
		t.Fatalf("lost disconnected output: %s", body)
	}
}

func TestIdentitySelectedBlockPreserved(t *testing.T) {
	svc, _ := serviceFor(t)
	root, _ := setupDir(t, "output \"Test Screen 0\" {\n scale 1\n layout { gaps 9; }\n}\n")
	if _, err := Setup(root); err != nil {
		t.Fatal(err)
	}
	svc, err := New(svc.socket, root, svc.validate)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Keep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body := readFile(t, svc.own.Managed); !strings.Contains(body, "gaps 9") {
		t.Fatalf("lost identity-selected block: %s", body)
	}
}

func TestTicketRejectsUnsafeMetadata(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	ticket := *svc.ticket
	ticket.Managed = filepath.Join(t.TempDir(), "unrelated.kdl")
	if err := ticket.Store(); err == nil {
		if _, err := LoadTicket(ticket.Token); err == nil {
			t.Fatal("accepted unrelated managed path")
		}
	}
}

func TestRecoveryFailureKeepsNonterminalTicket(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	dir, _ := TicketDir()
	if err := finish(dir, svc.ticket, StateReverted, "failed", errors.New("restore failed")); err == nil {
		t.Fatal("lost restore error")
	}
	ticket, err := LoadTicket(svc.ticket.Token)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.finished() {
		t.Fatal("failed recovery was marked complete")
	}
}

// A helper that never started saw no output command, so its ticket must not
// survive to block every later preview as unresolved.
func TestArmFailureLeavesNoUnresolvedTicket(t *testing.T) {
	svc, _ := serviceFor(t)
	start := startRecoveryUnit
	startRecoveryUnit = func(context.Context, string, string) error { return errors.New("no systemd") }
	err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}})
	startRecoveryUnit = start
	if err == nil {
		t.Fatal("preview succeeded without recovery")
	}
	if _, ok, err := activeTicketFor(svc.socket); err != nil || ok {
		t.Fatalf("an unarmed preview left a ticket: ok=%v err=%v", ok, err)
	}
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatalf("the next preview was blocked: %v", err)
	}
}

// The helper's attempt counter is a read-modify-write of the ticket, so it
// must wait for the decision lock a Keep holds.
func TestRecoveryStartWaitsForTheDecisionLock(t *testing.T) {
	svc, _ := serviceFor(t)
	window := armWindow
	armWindow = 2 * time.Second
	t.Cleanup(func() { armWindow = window })
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	dir := mustTicketDir()
	token := svc.ticket.Token
	if err := os.Remove(readyPath(dir, token)); err != nil {
		t.Fatal(err)
	}
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_ = withTicketLock(dir, token, func() error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	go func() { done <- RunRecovery(context.Background(), token) }()
	time.Sleep(200 * time.Millisecond)
	if tk, err := LoadTicket(token); err != nil || tk.Attempts != 0 {
		t.Fatalf("helper wrote the ticket while the lock was held: %+v, %v", tk, err)
	}
	if _, err := os.Stat(readyPath(dir, token)); err == nil {
		t.Fatal("helper reported ready while the lock was held")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("helper: %v", err)
	}
	// The helper resolved the preview at its deadline, so a later start finds
	// nothing and must exit cleanly rather than ask systemd for a restart.
	if err := RunRecovery(context.Background(), token); err != nil {
		t.Fatalf("a helper whose ticket was settled must exit cleanly: %v", err)
	}
}

func TestResolveRelativeUsesProducedSizes(t *testing.T) {
	live := map[string]niri.Output{
		"A": {Name: "A", Logical: &niri.LogicalOutput{X: 100, Y: 50, Width: 1536, Height: 864}},
		"B": {Name: "B", Logical: &niri.LogicalOutput{Width: 1280, Height: 720}},
	}
	for _, tc := range []struct {
		side string
		want Position
	}{
		{"right", Position{X: 1636, Y: 50}},
		{"left", Position{X: -1180, Y: 50}},
		{"below", Position{X: 100, Y: 914}},
		{"above", Position{X: 100, Y: -670}},
	} {
		target := map[string]Policy{"B": {Relative: &RelativePosition{To: "A", Side: tc.side}}}
		placed, err := resolveRelative(live, target)
		if err != nil {
			t.Fatal(err)
		}
		got := target["B"]
		if got.Relative != nil || got.Position == nil || *got.Position != tc.want || placed["B"].To != "A" {
			t.Errorf("%s: got %+v, want position %+v", tc.side, got, tc.want)
		}
	}
	// An anchor that is itself being pinned elsewhere anchors at its new place.
	target := map[string]Policy{
		"A": {Position: &Position{X: 0, Y: 0}},
		"B": {Relative: &RelativePosition{To: "A", Side: "right"}},
	}
	if _, err := resolveRelative(live, target); err != nil || *target["B"].Position != (Position{X: 1536, Y: 0}) {
		t.Errorf("pinned anchor: %+v, %v", target["B"].Position, err)
	}
	chained := map[string]Policy{
		"A": {Relative: &RelativePosition{To: "B", Side: "left"}},
		"B": {Relative: &RelativePosition{To: "A", Side: "right"}},
	}
	if _, err := resolveRelative(live, chained); err == nil {
		t.Error("a chain of relative placements was accepted")
	}
}

// A relative placement reaches the compositor and the kept file as the
// explicit position it resolved to, never as an automatic one.
func TestRelativePlacementIsAppliedAndKept(t *testing.T) {
	svc, fake := serviceFor(t)
	fake.mu.Lock()
	fake.outs["HDMI-A-1"] = &fakeOut{name: "HDMI-A-1", modes: []niriMode{{1280, 720, 60000}}, scale: 1, tx: "Normal"}
	fake.mu.Unlock()
	draft := map[string]Policy{"DP-1": {Relative: &RelativePosition{To: "HDMI-A-1", Side: "right"}}}
	if err := svc.Preview(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	x, y := fake.outs["DP-1"].x, fake.outs["DP-1"].y
	fake.mu.Unlock()
	if x != 1280 || y != 0 {
		t.Fatalf("DP-1 sits at %d,%d, want 1280,0", x, y)
	}
	if err := svc.Keep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if managed := readFile(t, svc.own.Managed); !strings.Contains(managed, "x=1280") {
		t.Fatalf("kept file lacks the resolved position:\n%s", managed)
	}
}

// Native validation must not hold the decision lock, or a slow validator
// would keep the helper from reverting at the deadline.
func TestKeepValidatesOutsideTheDecisionLock(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	token := svc.ticket.Token
	validate := svc.validate
	svc.validate = func(ctx context.Context, path string) error {
		probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		if err := withTicketLockContext(probe, mustTicketDir(), token, func() error { return nil }); err != nil {
			return fmt.Errorf("validation held the decision lock: %w", err)
		}
		return validate(ctx, path)
	}
	if err := svc.Keep(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A restarted shell reports the transaction its helper still owns instead of
// starting a competing preview.
func TestRestartedShellReportsAnUnresolvedPreview(t *testing.T) {
	svc, _ := serviceFor(t)
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(svc.socket, svc.root, svc.validate)
	if err != nil {
		t.Fatal(err)
	}
	if snap := snapshotOf(t, restarted); snap.Preview || snap.Unresolved == nil || snap.Unresolved.State != StatePreview {
		t.Fatalf("restarted snapshot = %+v, want the helper's preview reported as unresolved", snap)
	}
	if err := restarted.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(2.0)}}); err == nil {
		t.Fatal("a restarted shell started a competing preview")
	}
}
