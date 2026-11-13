package displays

import (
	"context"
	"errors"
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

func TestFailedPreviewKeepsRecoveryArmed(t *testing.T) {
	svc, fake := serviceFor(t)
	fake.mu.Lock()
	fake.failAfter = 2
	fake.mu.Unlock()
	if err := svc.Preview(context.Background(), map[string]Policy{"DP-1": {Scale: ptr(1.5)}}); err == nil {
		t.Fatal("broken preview succeeded")
	}
	if svc.ticket == nil {
		t.Fatal("failed restoration discarded recovery")
	}
	if _, err := LoadTicket(svc.ticket.Token); err != nil {
		t.Fatal(err)
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
