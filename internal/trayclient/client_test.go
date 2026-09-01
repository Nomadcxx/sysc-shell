package trayclient

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-tray/protocol"
)

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
	if !contains(hello.Capabilities, RequiredCapability) {
		t.Fatalf("client capabilities = %v", hello.Capabilities)
	}
	s.write(t, conn, envelopeOf(t, protocol.Envelope{Kind: protocol.KindHello}, protocol.Hello{
		Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor,
		Role: protocol.RolePresenter, Capabilities: []string{RequiredCapability},
	}))
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
		<-done
	})
	return client
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
	dir, err := os.MkdirTemp("", "syt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func envelopeOf(t *testing.T, envelope protocol.Envelope, payload any) []byte {
	t.Helper()
	frame, err := marshalEnvelope(envelope, payload)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func snapshotEnvelope(sequence uint64, items ...protocol.Item) []byte {
	snapshot := protocol.Snapshot{Sequence: sequence, Items: items}
	payload, _ := json.Marshal(snapshot)
	frame, _ := json.Marshal(protocol.Envelope{Kind: protocol.KindSnapshot, Sequence: sequence, Payload: payload})
	return frame
}

func itemEnvelope(kind string, sequence uint64, it protocol.Item) []byte {
	payload, _ := json.Marshal(it)
	frame, _ := json.Marshal(protocol.Envelope{Kind: kind, Sequence: sequence, Payload: payload})
	return frame
}

func replyEnvelope(requestID uint64, reply protocol.Reply) []byte {
	payload, _ := json.Marshal(reply)
	frame, _ := json.Marshal(protocol.Envelope{Kind: protocol.KindReply, RequestID: requestID, Payload: payload})
	return frame
}

func TestDialRejectsUnsafeSockets(t *testing.T) {
	if _, err := dial(""); err == nil {
		t.Fatal("empty runtime dir dialed")
	}
	// A world-writable socket is refused before any wire traffic.
	exposed := runtimeDir(t)
	path := filepath.Join(exposed, runtimeSubdir, socketName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := dial(exposed); err == nil {
		t.Fatal("a world-writable socket was accepted")
	}
}

func TestClientHandshakesThenAppliesSnapshotAndOrderedDeltas(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))

	snapshot := await(t, messages, KindSnapshot)
	if snapshot.Sequence != 4 || len(snapshot.Snapshot.Items) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if client.Generation() != snapshot.Generation {
		t.Fatalf("generation = %d, want %d", client.Generation(), snapshot.Generation)
	}

	service.write(t, conn, itemEnvelope(protocol.KindItemAdded, 5, item(2, "mic")))
	added := await(t, messages, KindItemAdded)
	if added.Sequence != 5 || added.Item.ID != "mic" {
		t.Fatalf("added = %+v", added)
	}
	if added.Generation != snapshot.Generation {
		t.Fatal("delta under a different generation")
	}
}

func TestClientEndsTheGenerationOnASequenceGap(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))
	await(t, messages, KindSnapshot)

	// Sequence 7 skips 5 and 6: the projection would silently miss two deltas.
	service.write(t, conn, itemEnvelope(protocol.KindItemAdded, 7, item(2, "gap")))
	got := await(t, messages, KindDisconnected)
	if got.Kind != KindDisconnected {
		t.Fatalf("gap did not end the generation: %+v", got)
	}
}

func TestClientEndsTheGenerationOnAMalformedFrame(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))
	await(t, messages, KindSnapshot)

	bad, _ := json.Marshal(protocol.Envelope{Kind: protocol.KindItemAdded, Sequence: 5,
		Payload: json.RawMessage(`{"key":{"owner":"","object_path":"","generation":0}}`)})
	service.write(t, conn, bad)
	await(t, messages, KindDisconnected)
}

func TestClientRefusesAReplyWithoutAPendingRequest(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))
	await(t, messages, KindSnapshot)

	// Request 99 was never sent: the stream is not the one we are speaking on.
	service.write(t, conn, replyEnvelope(99, protocol.Reply{OK: true}))
	await(t, messages, KindDisconnected)
}

func TestClientCorrelatesRepliesByRequestID(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))
	await(t, messages, KindSnapshot)

	id, err := client.Send(protocol.Command{Kind: protocol.CommandActivate, Item: item(1, "player").Key})
	if err != nil {
		t.Fatal(err)
	}
	// The service must see the command before its reply can correlate.
	_ = service.read(t, conn)
	service.write(t, conn, replyEnvelope(id, protocol.Reply{OK: true, Item: item(1, "player").Key}))
	got := await(t, messages, KindReply)
	if got.RequestID != id || !got.Reply.OK {
		t.Fatalf("reply = %+v", got)
	}
}

func TestClientBackpressureReturnsBusy(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 64)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "player")))
	await(t, messages, KindSnapshot)

	// Never read commands: the writer queue fills and Send reports busy. The
	// writer drains concurrently, so keep offering until it backs up.
	var lastErr error
	for i := 0; i < maxCommandQueue*4 && lastErr == nil; i++ {
		_, lastErr = client.Send(protocol.Command{Kind: protocol.CommandActivate, Item: item(1, "player").Key})
	}
	if lastErr != ErrBusy {
		t.Fatalf("full queue error = %v, want busy", lastErr)
	}
}

func TestClientReconnectsWithAFreshGeneration(t *testing.T) {
	service := startService(t)
	messages := make(chan Message, 32)
	client := run(t, service.dir, messages)

	conn := service.accept(t)
	service.handshake(t, conn)
	service.write(t, conn, snapshotEnvelope(4, item(1, "before")))
	first := await(t, messages, KindSnapshot)
	_ = conn.Close()
	await(t, messages, KindDisconnected)

	// The presenter was replaced: a new connection takes a new generation.
	conn2 := service.accept(t)
	service.handshake(t, conn2)
	service.write(t, conn2, snapshotEnvelope(1, item(9, "after")))
	second := await(t, messages, KindSnapshot)
	if second.Generation <= first.Generation {
		t.Fatalf("reconnect generation %d does not follow %d", second.Generation, first.Generation)
	}
	if second.Snapshot.Items[0].ID != "after" {
		t.Fatalf("reconnect snapshot = %+v", second.Snapshot)
	}
	if client.Generation() != second.Generation {
		t.Fatalf("Generation() = %d, want %d", client.Generation(), second.Generation)
	}
}
