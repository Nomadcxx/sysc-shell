package services

import (
	"testing"
	"time"
)

// The slot is pure state with no D-Bus in it, which is why the whole
// single-slot contract is testable without a bus.

func TestSecondRequestIsRejectedWhileOneIsPending(t *testing.T) {
	s := newSecretSlot()

	first := make(chan secretReply, 1)
	if !s.begin(SecretRequest{SSID: "Orac 15A"}, first) {
		t.Fatal("the first request must be accepted")
	}

	second := make(chan secretReply, 1)
	if s.begin(SecretRequest{SSID: "LukeAP"}, second) {
		t.Fatal("a second request must be refused while one is pending")
	}
	select {
	case got := <-second:
		if !got.noSecrets {
			t.Error("the refusal must answer NoSecrets so NetworkManager falls back to its own store")
		}
	case <-time.After(time.Second):
		t.Fatal("the refusal must reply immediately, not queue behind the open prompt")
	}
}

// A dangling reply hangs NetworkManager's activation until its own timeout,
// which presents to the user as a join that silently does nothing.
func TestCancelAlwaysReplies(t *testing.T) {
	s := newSecretSlot()
	ch := make(chan secretReply, 1)
	s.begin(SecretRequest{SSID: "Orac 15A"}, ch)

	s.cancel()
	select {
	case got := <-ch:
		if !got.cancelled {
			t.Error("cancel must reply UserCanceled")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel left the request dangling")
	}
}

func TestSubmitDeliversTheSecretAndFreesTheSlot(t *testing.T) {
	s := newSecretSlot()
	ch := make(chan secretReply, 1)
	s.begin(SecretRequest{SSID: "Orac 15A"}, ch)

	s.submit("hunter2")
	select {
	case got := <-ch:
		if got.psk != "hunter2" {
			t.Errorf("psk = %q, want the submitted value", got.psk)
		}
		if got.noSecrets || got.cancelled {
			t.Error("a submitted secret is neither a refusal nor a cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("submit never replied")
	}

	// The slot frees, so the next join can prompt.
	next := make(chan secretReply, 1)
	if !s.begin(SecretRequest{SSID: "LukeAP"}, next) {
		t.Error("the slot must accept a new request once the last one is answered")
	}
}

// Exactly one reply per request: a second answer would write to a channel
// NetworkManager has already stopped reading.
func TestSecondAnswerIsIgnored(t *testing.T) {
	s := newSecretSlot()
	ch := make(chan secretReply, 1)
	s.begin(SecretRequest{SSID: "Orac 15A"}, ch)

	s.submit("hunter2")
	<-ch // the one legitimate answer

	s.cancel()
	s.submit("again")

	if n := len(ch); n != 0 {
		t.Errorf("%d extra replies queued; a request is answered once", n)
	}
}

// Closing a panel that never prompted must not panic or reply to nothing.
func TestCancelOnAnEmptySlotIsANoOp(t *testing.T) {
	s := newSecretSlot()
	s.cancel()
	s.submit("nothing pending")
	if s.pending() {
		t.Error("an empty slot must not report a pending request")
	}
}

func TestPendingReportsWhileAPromptIsOpen(t *testing.T) {
	s := newSecretSlot()
	if s.pending() {
		t.Fatal("a fresh slot has nothing pending")
	}
	s.begin(SecretRequest{SSID: "Orac 15A"}, make(chan secretReply, 1))
	if !s.pending() {
		t.Error("the slot must report the open prompt")
	}
	s.cancel()
	if s.pending() {
		t.Error("the slot must clear once answered")
	}
}

func TestNetworkCloseCancelsPendingSecret(t *testing.T) {
	n := NewNetwork(&fakeBackend{})
	reply := make(chan secretReply, 1)
	n.secrets.begin(SecretRequest{SSID: "Orac 15A"}, reply)

	n.Close()

	select {
	case got := <-reply:
		if !got.cancelled {
			t.Error("closing the service must cancel its pending credential request")
		}
	case <-time.After(time.Second):
		t.Fatal("closing the service left a credential request dangling")
	}
}

func TestNetworkCloseReleasesSecretExportOnce(t *testing.T) {
	n := NewNetwork(&fakeBackend{})
	closed := 0
	n.closeSecrets = func() { closed++ }

	n.Close()
	n.Close()

	if closed != 1 {
		t.Fatalf("credential export released %d times, want once", closed)
	}
}

func TestStartSecretsWiresServiceSlotAndLifetime(t *testing.T) {
	n := NewNetwork(&fakeBackend{})
	closed := 0
	err := n.startSecrets(func(export *secretExport) (func(), error) {
		if export.slot != n.secrets || export.requests != n.secretReqs {
			t.Fatal("credential export must use the service's slot and request channel")
		}
		return func() { closed++ }, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	n.Close()
	if closed != 1 {
		t.Fatalf("credential export released %d times, want once", closed)
	}
}
