package notifyclient

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
)

// These fixtures pin the wire contract to the imported protocol package. The
// shell defines no wire structs of its own: if a copy were reintroduced, these
// would still pass against the copy, so every fixture names protocol types
// directly and the package must contain no struct with json tags.

func TestFixtureHelloRoundTrips(t *testing.T) {
	hello := protocol.Hello{
		Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor,
		Role: protocol.RolePresenter, Capabilities: []string{"notification-state", "actions"},
	}
	var got protocol.Hello
	roundTrip(t, hello, &got)
	if err := got.Validate(protocol.RolePresenter); err != nil {
		t.Fatal(err)
	}
	if got.Major != protocol.ProtocolMajor || len(got.Capabilities) != 2 {
		t.Fatalf("hello = %+v", got)
	}
	if err := (protocol.Hello{Major: protocol.ProtocolMajor + 1, Role: protocol.RolePresenter}).
		Validate(protocol.RolePresenter); err == nil {
		t.Fatal("an incompatible major was accepted")
	}
}

func TestFixtureSnapshotCarriesActiveAndHistory(t *testing.T) {
	value := int32(40)
	snapshot := protocol.Snapshot{
		Sequence: 9,
		Active: []protocol.Notification{{
			ID: 1, AppName: "chat", AppIcon: "chat", DesktopEntry: "chat.desktop",
			Summary: "Message", Body: "Body text", Category: "im.received",
			Urgency: protocol.UrgencyCritical, Timestamp: time.Unix(1700000000, 0).UTC(),
			ExpireTimeoutMS: 5000, InlineReply: true, Value: &value,
			Actions: []protocol.Action{{Key: "reply", Label: "Reply"}},
			Image: &protocol.Image{
				MediaType: "image/png", Width: 64, Height: 64, Data: []byte{0x89, 'P', 'N', 'G'},
			},
			SenderLineage: []protocol.Process{{PID: 42, StartTime: 991}, {PID: 1, StartTime: 2}},
		}},
		Lifetimes: []protocol.Lifetime{{ID: 1, DurationMS: 5000, RemainingMS: 3000, Running: true}},
		History: []protocol.HistoryEntry{{
			ID: 2, Seen: true, AppName: "mail", Summary: "Old",
			Urgency: protocol.UrgencyLow, Timestamp: time.Unix(1699999000, 0).UTC(),
		}},
	}
	var got protocol.Snapshot
	roundTrip(t, snapshot, &got)
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Sequence != 9 || len(got.Active) != 1 || len(got.History) != 1 {
		t.Fatalf("snapshot = %+v", got)
	}
	active := got.Active[0]
	if active.Urgency != protocol.UrgencyCritical || !active.InlineReply {
		t.Fatalf("active enums = %+v", active)
	}
	if active.Value == nil || *active.Value != 40 {
		t.Fatalf("progress value = %v", active.Value)
	}
	if len(active.SenderLineage) != 2 || active.SenderLineage[0].PID != 42 {
		t.Fatalf("lineage = %+v", active.SenderLineage)
	}
	if active.Image == nil || active.Image.MediaType != "image/png" || len(active.Image.Data) != 4 {
		t.Fatalf("image = %+v", active.Image)
	}
	if len(active.Actions) != 1 || active.Actions[0].Key != "reply" {
		t.Fatalf("actions = %+v", active.Actions)
	}
	if len(got.Lifetimes) != 1 || got.Lifetimes[0].RemainingMS != 3000 || !got.Lifetimes[0].Running {
		t.Fatalf("lifetimes = %+v", got.Lifetimes)
	}
	if !got.History[0].Seen {
		t.Fatal("history seen state did not survive the round trip")
	}
}

func TestFixtureEveryDeltaRoundTrips(t *testing.T) {
	record := protocol.Notification{
		ID: 3, Summary: "Delta", Urgency: protocol.UrgencyNormal,
		Timestamp: time.Unix(1700000001, 0).UTC(),
	}
	lifetime := protocol.Lifetime{ID: 3, DurationMS: 5000, RemainingMS: 5000, Running: true}
	entry := protocol.HistoryEntry{
		ID: 4, Summary: "Filed", Urgency: protocol.UrgencyNormal,
		Timestamp: time.Unix(1700000002, 0).UTC(),
	}
	for _, delta := range []protocol.Delta{
		{Kind: protocol.DeltaAdded, Notification: &record, Lifetime: &lifetime},
		{Kind: protocol.DeltaReplaced, Notification: &record, Lifetime: &lifetime},
		{Kind: protocol.DeltaClosed, ID: 3, CloseReason: protocol.CloseDismissed},
		{Kind: protocol.DeltaHistoryAdded, History: &entry},
		{Kind: protocol.DeltaHistoryRemoved, ID: 4},
		{Kind: protocol.DeltaHistorySeen, IDs: []uint32{4}},
		{Kind: protocol.DeltaHistoryCleared},
	} {
		t.Run(string(delta.Kind), func(t *testing.T) {
			var got protocol.Delta
			roundTrip(t, delta, &got)
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			if got.Kind != delta.Kind {
				t.Fatalf("kind = %q, want %q", got.Kind, delta.Kind)
			}
		})
	}

	closed := protocol.Delta{Kind: protocol.DeltaClosed, ID: 3, CloseReason: protocol.CloseExpired}
	var got protocol.Delta
	roundTrip(t, closed, &got)
	if got.CloseReason != protocol.CloseExpired {
		t.Fatalf("close reason = %d", got.CloseReason)
	}
	for _, reason := range []protocol.CloseReason{
		protocol.CloseExpired, protocol.CloseDismissed,
		protocol.CloseRequested, protocol.CloseUndefined,
	} {
		if err := (protocol.Delta{Kind: protocol.DeltaClosed, ID: 1, CloseReason: reason}).Validate(); err != nil {
			t.Fatalf("close reason %d: %v", reason, err)
		}
	}
}

func TestFixtureEveryCommandAndReplyRoundTrips(t *testing.T) {
	for _, command := range []protocol.Command{
		{Kind: protocol.CommandAction, ID: 1, ActionKey: "reply"},
		{Kind: protocol.CommandDismiss, ID: 1},
		{Kind: protocol.CommandReply, ID: 1, Text: "on my way"},
		{Kind: protocol.CommandHistoryClear},
		{Kind: protocol.CommandHistoryMarkSeen, IDs: []uint32{1, 2}},
		{Kind: protocol.CommandDismissAll},
		{Kind: protocol.CommandPresentationRenew, Presentations: []protocol.Presentation{
			{ID: 1, State: protocol.PresentationHovered},
			{ID: 2, State: protocol.PresentationVisible},
			{ID: 3, State: protocol.PresentationQueued},
			{ID: 4, State: protocol.PresentationSuppressed},
		}},
	} {
		t.Run(string(command.Kind), func(t *testing.T) {
			var got protocol.Command
			roundTrip(t, command, &got)
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			if got.Kind != command.Kind {
				t.Fatalf("kind = %q, want %q", got.Kind, command.Kind)
			}
		})
	}

	var renew protocol.Command
	roundTrip(t, protocol.Command{Kind: protocol.CommandPresentationRenew,
		Presentations: []protocol.Presentation{{ID: 7, State: protocol.PresentationSuppressed}}}, &renew)
	if len(renew.Presentations) != 1 || renew.Presentations[0].State != protocol.PresentationSuppressed {
		t.Fatalf("presentation states = %+v", renew.Presentations)
	}

	var ok protocol.Reply
	roundTrip(t, protocol.Reply{OK: true, Lifetimes: []protocol.Lifetime{{ID: 7, DurationMS: 5000, RemainingMS: 2500}}}, &ok)
	if err := ok.Validate(); err != nil || !ok.OK {
		t.Fatalf("ok reply = %+v (%v)", ok, err)
	}
	if len(ok.Lifetimes) != 1 || ok.Lifetimes[0].RemainingMS != 2500 {
		t.Fatalf("reply lifetimes = %+v", ok.Lifetimes)
	}
	for _, code := range []protocol.ErrorCode{
		protocol.ErrorInvalid, protocol.ErrorNotFound,
		protocol.ErrorStale, protocol.ErrorBusy, protocol.ErrorUnavailable,
	} {
		var failed protocol.Reply
		roundTrip(t, protocol.Reply{Error: &protocol.ProtocolError{Code: code, Message: "no"}}, &failed)
		if err := failed.Validate(); err != nil {
			t.Fatalf("%q: %v", code, err)
		}
		if failed.Error == nil || failed.Error.Code != code {
			t.Fatalf("error code = %+v, want %q", failed.Error, code)
		}
	}
	if err := (protocol.Reply{OK: true, Error: &protocol.ProtocolError{Code: protocol.ErrorBusy}}).
		Validate(); err == nil {
		t.Fatal("a reply carrying two outcomes was accepted")
	}
}

func TestFixtureEnvelopeSequenceAndRequestIDRules(t *testing.T) {
	payload := json.RawMessage(`{}`)
	for _, envelope := range []protocol.Envelope{
		{Kind: protocol.KindHello, Payload: payload},
		{Kind: protocol.KindSnapshot, Sequence: 4, Payload: payload},
		{Kind: protocol.KindAdded, Sequence: 5, Payload: payload},
		{Kind: protocol.KindCommand, RequestID: 1, Payload: payload},
		{Kind: protocol.KindReply, RequestID: 1, Payload: payload},
	} {
		if err := envelope.Validate(); err != nil {
			t.Fatalf("%q: %v", envelope.Kind, err)
		}
	}
	for name, envelope := range map[string]protocol.Envelope{
		"unknown kind":           {Kind: "future", Payload: payload},
		"delta without sequence": {Kind: protocol.KindAdded, Payload: payload},
		"delta with request ID":  {Kind: protocol.KindAdded, Sequence: 1, RequestID: 1, Payload: payload},
		"command without ID":     {Kind: protocol.KindCommand, Payload: payload},
		"hello with sequence":    {Kind: protocol.KindHello, Sequence: 1, Payload: payload},
		"empty payload":          {Kind: protocol.KindHello},
	} {
		t.Run(name, func(t *testing.T) {
			if err := envelope.Validate(); err == nil {
				t.Fatalf("Validate accepted %+v", envelope)
			}
		})
	}

	if err := protocol.ValidateNextSequence(4, 5); err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateNextSequence(4, 6); err == nil {
		t.Fatal("a sequence gap was accepted")
	}
	if err := protocol.ValidateNextSequence(4, 4); err == nil {
		t.Fatal("a repeated sequence was accepted")
	}
}

func TestFixtureRejectsUnknownFieldsAndBounds(t *testing.T) {
	var envelope protocol.Envelope
	if err := protocol.DecodeStrict([]byte(`{"kind":"hello","payload":{},"extra":1}`), &envelope); err == nil {
		t.Fatal("an unknown envelope field was accepted")
	}
	if err := protocol.DecodeStrict([]byte(`{"kind":"hello","kind":"snapshot","payload":{}}`), &envelope); err == nil {
		t.Fatal("a duplicate key was accepted")
	}

	oversized := protocol.Notification{
		ID: 1, Summary: strings.Repeat("x", protocol.MaxBodyBytes+1),
		Urgency: protocol.UrgencyNormal, Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	if err := oversized.Validate(); err == nil {
		t.Fatal("an oversized summary was accepted")
	}
	tooManyActions := protocol.Notification{
		ID: 1, Summary: "s", Urgency: protocol.UrgencyNormal,
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Actions:   make([]protocol.Action, protocol.MaxActionPairs+1),
	}
	if err := tooManyActions.Validate(); err == nil {
		t.Fatal("too many actions were accepted")
	}
	longLineage := protocol.Notification{
		ID: 1, Summary: "s", Urgency: protocol.UrgencyNormal,
		Timestamp:     time.Unix(1700000000, 0).UTC(),
		SenderLineage: make([]protocol.Process, protocol.MaxLineageEntries+1),
	}
	if err := longLineage.Validate(); err == nil {
		t.Fatal("an over-long lineage was accepted")
	}
	wideImage := protocol.Image{
		MediaType: "image/png", Width: protocol.MaxWireImageLongEdge + 1, Height: 1, Data: []byte{1},
	}
	if err := wideImage.Validate(); err == nil {
		t.Fatal("an oversized wire image was accepted")
	}
	if err := (protocol.Snapshot{Active: make([]protocol.Notification, protocol.MaxActiveNotifications+1)}).
		Validate(); err == nil {
		t.Fatal("a snapshot past the active cap was accepted")
	}
}

// roundTrip encodes through JSON and back, the way a frame travels.
func roundTrip(t *testing.T, in, out any) {
	t.Helper()
	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.DecodeStrict(encoded, out); err != nil {
		t.Fatalf("DecodeStrict(%s): %v", encoded, err)
	}
}
