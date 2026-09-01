package notifyclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
)

func TestClientRefusesUnsafeSocketPaths(t *testing.T) {
	if _, err := dial(""); err == nil {
		t.Fatal("an empty runtime directory was accepted")
	}

	loose := runtimeDir(t)
	writeFakeSocket(t, loose, 0o600)
	if err := os.Chmod(filepath.Join(loose, runtimeSubdir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := dial(loose); err == nil {
		t.Fatal("a world-readable runtime directory was accepted")
	}

	exposed := runtimeDir(t)
	writeFakeSocket(t, exposed, 0o666)
	if _, err := dial(exposed); err == nil {
		t.Fatal("a world-writable socket was accepted")
	}

	linked := runtimeDir(t)
	target := filepath.Join(linked, runtimeSubdir)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(target, socketName)); err != nil {
		t.Fatal(err)
	}
	if _, err := dial(linked); err == nil {
		t.Fatal("a symlinked socket was accepted")
	}
}

func TestClientHandshakesThenAppliesSnapshotAndOrderedDeltas(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, "first"))

	snapshot := await(t, messages, KindSnapshot)
	if snapshot.Sequence != 4 || len(snapshot.Snapshot.Active) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Snapshot.Active[0].Summary != "first" {
		t.Fatalf("snapshot summary = %q", snapshot.Snapshot.Active[0].Summary)
	}
	if client.Generation() != snapshot.Generation {
		t.Fatalf("generation = %d, want %d", client.Generation(), snapshot.Generation)
	}

	service.write(t, conn, deltaEnvelope(5, protocol.Delta{
		Kind: protocol.DeltaClosed, ID: 1, CloseReason: protocol.CloseDismissed,
	}))
	delta := await(t, messages, KindDelta)
	if delta.Sequence != 5 || delta.Delta.Kind != protocol.DeltaClosed {
		t.Fatalf("delta = %+v", delta)
	}
	if delta.Generation != snapshot.Generation {
		t.Fatal("a delta arrived under a different generation than its snapshot")
	}
}

func TestClientRejectsServiceWithoutLifetimeCapability(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshakeCapabilities(t, conn, []string{RequiredCapability})
	service.write(t, conn, snapshotEnvelope(1, "missing lifetime capability"))
	select {
	case got := <-messages:
		t.Fatalf("service without lifetime capability produced %v", got.Kind)
	case <-time.After(200 * time.Millisecond):
	}

	replacement := service.accept(t)
	service.handshake(t, replacement)
	service.write(t, replacement, snapshotEnvelope(2, "capable service"))
	if got := await(t, messages, KindSnapshot); got.Snapshot.Active[0].Summary != "capable service" {
		t.Fatalf("replacement snapshot = %+v", got.Snapshot)
	}
}

func TestClientEndsTheGenerationOnASequenceGap(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, "first"))
	await(t, messages, KindSnapshot)

	// Sequence 6 skips 5: the shell cannot know what it missed.
	service.write(t, conn, deltaEnvelope(6, protocol.Delta{
		Kind: protocol.DeltaClosed, ID: 1, CloseReason: protocol.CloseExpired,
	}))
	if got := await(t, messages, KindDisconnected); got.Kind != KindDisconnected {
		t.Fatalf("a sequence gap produced %v", got.Kind)
	}
}

func TestClientEndsTheGenerationOnAMalformedFrame(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(1, "first"))
	await(t, messages, KindSnapshot)

	if err := protocol.WriteFrame(conn, []byte(`{"kind":"added","sequence":2,`)); err != nil {
		t.Fatal(err)
	}
	await(t, messages, KindDisconnected)
}

func TestClientRejectsDeltasBeforeAnyBaseline(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, deltaEnvelope(1, protocol.Delta{
		Kind: protocol.DeltaClosed, ID: 1, CloseReason: protocol.CloseExpired,
	}))
	await(t, messages, KindDisconnected)
}

func TestClientReconnectsAndTakesAFreshBaseline(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 64)
	client := run(t, service.dir, messages)

	first := service.accept(t)
	service.handshake(t, first)
	service.write(t, first, snapshotEnvelope(2, "before"))
	before := await(t, messages, KindSnapshot)
	_ = first.Close()
	await(t, messages, KindDisconnected)

	second := service.accept(t)
	service.handshake(t, second)
	service.write(t, second, snapshotEnvelope(9, "after"))
	after := await(t, messages, KindSnapshot)

	if after.Generation <= before.Generation {
		t.Fatalf("reconnect generation %d does not follow %d", after.Generation, before.Generation)
	}
	if after.Snapshot.Active[0].Summary != "after" {
		t.Fatalf("reconnect snapshot = %+v", after.Snapshot.Active)
	}
	if client.Generation() != after.Generation {
		t.Fatalf("Generation() = %d, want %d", client.Generation(), after.Generation)
	}
}

func TestClientCorrelatesRepliesAndRefusesUnknownOnes(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(1, "first"))
	await(t, messages, KindSnapshot)

	requestID, err := client.Send(protocol.Command{Kind: protocol.CommandDismiss, ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	command := service.read(t, conn)
	if command.Kind != protocol.KindCommand || command.RequestID != requestID {
		t.Fatalf("command envelope = %+v, want request %d", command, requestID)
	}

	service.write(t, conn, replyEnvelope(requestID, protocol.Reply{OK: true}))
	reply := await(t, messages, KindReply)
	if reply.RequestID != requestID || !reply.Reply.OK {
		t.Fatalf("reply = %+v", reply)
	}

	// A second reply for the same request names no outstanding command.
	service.write(t, conn, replyEnvelope(requestID, protocol.Reply{OK: true}))
	await(t, messages, KindDisconnected)
}

func TestClientReportsBusyWhenTheCommandQueueIsFull(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(1, "first"))
	await(t, messages, KindSnapshot)

	// The service never reads, so the queue fills and then refuses.
	var busy error
	for i := 0; i < protocol.MaxCommandQueue*4 && busy == nil; i++ {
		_, busy = client.Send(protocol.Command{Kind: protocol.CommandDismiss, ID: uint32(i + 1)})
	}
	if !errors.Is(busy, ErrBusy) {
		t.Fatalf("Send eventually returned %v, want ErrBusy", busy)
	}
}

func TestClientRefusesInvalidCommandsWithoutQueueingThem(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(1, "first"))
	await(t, messages, KindSnapshot)

	if _, err := client.Send(protocol.Command{Kind: "teleport"}); err == nil {
		t.Fatal("an unknown command kind was queued")
	}
	if _, err := client.Send(protocol.Command{Kind: protocol.CommandDismiss}); err == nil {
		t.Fatal("a dismiss without an ID was queued")
	}
}

func TestClientRefusesToSendWhileDetached(t *testing.T) {
	client := New(runtimeDir(t), nil)
	if _, err := client.Send(protocol.Command{Kind: protocol.CommandDismiss, ID: 1}); err == nil {
		t.Fatal("a command was accepted with no connection")
	}
	if client.Generation() != 0 {
		t.Fatalf("Generation() = %d while detached", client.Generation())
	}
}

// fakeService is the presenter socket the real service would bind.
type fakeService struct {
	dir      string
	listener *net.UnixListener
}

func startService(t *testing.T) *fakeService {
	t.Helper()
	dir := runtimeDir(t)
	path := filepath.Join(dir, runtimeSubdir, socketName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return &fakeService{dir: dir, listener: listener}
}

func (s *fakeService) accept(t *testing.T) *net.UnixConn {
	t.Helper()
	if err := s.listener.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := s.listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func (s *fakeService) handshake(t *testing.T, conn *net.UnixConn) {
	s.handshakeCapabilities(t, conn, []string{RequiredCapability, RequiredLifetimeCapability})
}

func (s *fakeService) handshakeCapabilities(t *testing.T, conn *net.UnixConn, capabilities []string) {
	t.Helper()
	envelope := s.read(t, conn)
	if envelope.Kind != protocol.KindHello {
		t.Fatalf("client sent %q first, want hello", envelope.Kind)
	}
	var hello protocol.Hello
	if err := protocol.DecodeStrict(envelope.Payload, &hello); err != nil {
		t.Fatal(err)
	}
	if err := hello.Validate(protocol.RolePresenter); err != nil {
		t.Fatal(err)
	}
	if !contains(hello.Capabilities, RequiredCapability) || !contains(hello.Capabilities, RequiredLifetimeCapability) {
		t.Fatalf("client capabilities = %v", hello.Capabilities)
	}
	s.write(t, conn, envelopeOf(t, protocol.Envelope{Kind: protocol.KindHello}, protocol.Hello{
		Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor,
		Role: protocol.RolePresenter, Capabilities: capabilities,
	}))
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (s *fakeService) write(t *testing.T, conn *net.UnixConn, frame []byte) {
	t.Helper()
	if err := protocol.WriteFrame(conn, frame); err != nil {
		t.Fatal(err)
	}
}

func (s *fakeService) read(t *testing.T, conn *net.UnixConn) protocol.Envelope {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	frame, err := protocol.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	var envelope protocol.Envelope
	if err := protocol.DecodeStrict(frame, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func run(t *testing.T, dir string, messages chan Message) *Client {
	t.Helper()
	client := New(dir, messages)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = client.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		drain(messages)
		<-done
	})
	return client
}

func drain(messages chan Message) {
	var once sync.Once
	stop := make(chan struct{})
	once.Do(func() { close(stop) })
	go func() {
		for {
			select {
			case <-messages:
			case <-stop:
				return
			}
		}
	}()
}

func await(t *testing.T, messages chan Message, kind Kind) Message {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case message := <-messages:
			if message.Kind == kind {
				return message
			}
			if message.Kind == KindDisconnected {
				t.Fatalf("connection ended while waiting for %v", kind)
			}
		case <-deadline:
			t.Fatalf("no %v arrived", kind)
		}
	}
}

func runtimeDir(t *testing.T) string {
	t.Helper()
	// Unix socket addresses are capped near 108 bytes, so the base stays short.
	dir, err := os.MkdirTemp("", "syn")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeFakeSocket(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, runtimeSubdir, socketName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func envelopeOf(t *testing.T, envelope protocol.Envelope, payload any) []byte {
	t.Helper()
	frame, err := marshalEnvelope(envelope, payload)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func snapshotEnvelope(sequence uint64, summary string) []byte {
	snapshot := protocol.Snapshot{Sequence: sequence, Active: []protocol.Notification{{
		ID: 1, Summary: summary, Urgency: protocol.UrgencyNormal,
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}}, Lifetimes: []protocol.Lifetime{{ID: 1, DurationMS: 5000, RemainingMS: 5000, Running: true}}}
	payload, _ := json.Marshal(snapshot)
	frame, _ := json.Marshal(protocol.Envelope{
		Kind: protocol.KindSnapshot, Sequence: sequence, Payload: payload,
	})
	return frame
}

func deltaEnvelope(sequence uint64, delta protocol.Delta) []byte {
	payload, _ := json.Marshal(delta)
	frame, _ := json.Marshal(protocol.Envelope{
		Kind: string(delta.Kind), Sequence: sequence, Payload: payload,
	})
	return frame
}

func replyEnvelope(requestID uint64, reply protocol.Reply) []byte {
	payload, _ := json.Marshal(reply)
	frame, _ := json.Marshal(protocol.Envelope{
		Kind: protocol.KindReply, RequestID: requestID, Payload: payload,
	})
	return frame
}
