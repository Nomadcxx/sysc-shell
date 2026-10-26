package services

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// Each prompt owns its answer. A passphrase typed for a prompt that already
// closed must reach that prompt only, never the one that took its place:
// otherwise the user hands one network's secret to another.
func TestALatePassphraseDoesNotReachTheNextPrompt(t *testing.T) {
	s := newSecretSlot()

	first := make(chan secretReply, 1)
	firstReq, firstGen, ok := s.begin(SecretRequest{SSID: "Orac 15A"}, first)
	if !ok {
		t.Fatal("the first request must be accepted")
	}
	if firstReq.Generation != firstGen {
		t.Fatalf("the shown request carries generation %d, want %d", firstReq.Generation, firstGen)
	}
	if !s.submitGen(firstGen, "hunter2") {
		t.Fatal("answering the open prompt must succeed")
	}

	second := make(chan secretReply, 1)
	_, secondGen, ok := s.begin(SecretRequest{SSID: "LukeAP"}, second)
	if !ok {
		t.Fatal("the answered prompt must free the slot")
	}
	if secondGen == firstGen {
		t.Fatal("two prompts must not share a generation")
	}

	if s.submitGen(firstGen, "late") {
		t.Fatal("a passphrase for the closed prompt reached the prompt that took its place")
	}
	if !s.pending() {
		t.Fatal("the late passphrase closed the prompt that is open")
	}
	select {
	case got := <-second:
		t.Fatalf("the open prompt was answered by a late one: %+v", got)
	default:
	}

	if !s.submitGen(secondGen, "right") {
		t.Fatal("the open prompt refused its own answer")
	}
	select {
	case got := <-second:
		if got.psk != "right" {
			t.Fatalf("the open prompt got %q, want %q", got.psk, "right")
		}
	default:
		t.Fatal("the open prompt was never answered")
	}
}

// A cancel meant for a prompt that already closed must not cancel the prompt
// that took its place.
func TestALateCancelDoesNotReachTheNextPrompt(t *testing.T) {
	s := newSecretSlot()

	first := make(chan secretReply, 1)
	_, firstGen, ok := s.begin(SecretRequest{SSID: "Orac 15A"}, first)
	if !ok {
		t.Fatal("the first request must be accepted")
	}
	if !s.cancelGen(firstGen) {
		t.Fatal("cancelling the open prompt must succeed")
	}

	second := make(chan secretReply, 1)
	if _, _, ok := s.begin(SecretRequest{SSID: "LukeAP"}, second); !ok {
		t.Fatal("the cancelled prompt must free the slot")
	}

	if s.cancelGen(firstGen) {
		t.Fatal("a cancel for the closed prompt reached the prompt that took its place")
	}
	if !s.pending() {
		t.Fatal("the late cancel closed the prompt that is open")
	}
	select {
	case got := <-second:
		t.Fatalf("the open prompt was cancelled by a late one: %+v", got)
	default:
	}
}

// The panel names the prompt it answers. A generation that no longer holds the
// prompt is told so, so the card can go instead of asking again for a secret
// nobody waits for.
func TestSubmitSecretRefusesAClosedPrompt(t *testing.T) {
	n := NewNetwork(&fakeBackend{})
	_, gen, ok := n.secrets.begin(SecretRequest{SSID: "Orac 15A"}, make(chan secretReply, 1))
	if !ok {
		t.Fatal("the slot must accept the first request")
	}
	if err := n.SubmitSecret(gen, "hunter22"); err != nil {
		t.Fatalf("answering the open prompt returned %v", err)
	}
	err := n.SubmitSecret(gen, "hunter22")
	if err != ErrSecretPromptClosed {
		t.Fatalf("a late submit returned %v, want ErrSecretPromptClosed", err)
	}
}

// NetworkManager withdraws the question it asked. The withdraw names the
// connection, so it must not cancel a prompt that belongs to another one.
func TestAWithdrawalDoesNotCancelAnotherConnectionsPrompt(t *testing.T) {
	requests := make(chan SecretRequest, 1)
	expired := make(chan uint64, 1)
	export := &secretExport{
		slot: newSecretSlot(), requests: requests,
		timeout: time.Minute, expired: expired,
	}
	ask := func(connection string) (<-chan *dbus.Error, SecretRequest) {
		done := make(chan *dbus.Error, 1)
		go func() {
			_, err := export.GetSecrets(secretWireRequest(), dbus.ObjectPath(connection), wirelessSecuritySetting, nil, flagAllowInteraction)
			done <- err
		}()
		return done, <-requests
	}

	first, firstReq := ask("/connection/orac")
	if firstReq.Connection != "/connection/orac" {
		t.Fatalf("the request names connection %q, want %q", firstReq.Connection, "/connection/orac")
	}
	if !export.slot.submitGen(firstReq.Generation, "hunter2") {
		t.Fatal("answering the open prompt must succeed")
	}
	if err := <-first; err != nil {
		t.Fatalf("the first prompt returned %v, want success", err)
	}

	second, secondReq := ask("/connection/luke")
	if secondReq.Generation == firstReq.Generation {
		t.Fatal("two prompts must not share a generation")
	}

	// A withdraw for the connection that was already answered leaves the
	// prompt that is open alone.
	if err := export.CancelGetSecrets("/connection/orac", wirelessSecuritySetting); err != nil {
		t.Fatalf("the withdrawal returned %v", err)
	}
	if !export.slot.pending() {
		t.Fatal("a withdrawal for an answered connection cancelled the open prompt")
	}
	select {
	case err := <-second:
		t.Fatalf("the open prompt was cancelled by a late withdrawal: %v", err)
	default:
	}

	// The withdrawal for the connection that is open ends its prompt.
	if err := export.CancelGetSecrets("/connection/luke", wirelessSecuritySetting); err != nil {
		t.Fatalf("the withdrawal returned %v", err)
	}
	select {
	case err := <-second:
		if err == nil || err.Name != errUserCanceled {
			t.Fatalf("the withdrawn prompt returned %v, want UserCanceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the withdrawal left GetSecrets hanging")
	}
}
