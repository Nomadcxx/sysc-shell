package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// The credential path. This is the one piece the pinned binding does not
// provide: it is a client, and answering GetSecrets means exporting an object
// of our own, so this file talks to godbus directly.
const (
	secretExportPath  = dbus.ObjectPath("/org/freedesktop/NetworkManager/SecretAgent")
	secretExportIface = "org.freedesktop.NetworkManager.SecretAgent"
	agentManagerPath  = dbus.ObjectPath("/org/freedesktop/NetworkManager/AgentManager")
	agentManagerIface = "org.freedesktop.NetworkManager.AgentManager"
	networkManagerBus = "org.freedesktop.NetworkManager"

	// The identifier NetworkManager files this holder under.
	secretHolderID = "one.archpcx.sysc-shell"

	// wirelessSecuritySetting is the only setting answered here. An 802.1X
	// request is refused, which routes the user to a full-featured holder.
	wirelessSecuritySetting = "802-11-wireless-security"

	errNoSecrets    = secretExportIface + ".Error.NoSecrets"
	errUserCanceled = secretExportIface + ".Error.UserCanceled"

	// NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION
	flagAllowInteraction uint32 = 0x1
)

// SecretRequest is NetworkManager asking for a passphrase.
type SecretRequest struct {
	SSID        string
	SettingName string
}

// secretReply is the single answer a request receives.
type secretReply struct {
	psk       string
	noSecrets bool
	cancelled bool
}

// secretSlot holds at most one in-flight prompt.
//
// Single-slot is deliberate, copying the reference shell: a concurrent request
// is answered NoSecrets so NetworkManager falls back to its own store rather
// than queueing a second prompt behind the first, which the user cannot see.
//
// It contains no D-Bus, which is why the whole contract is testable without a
// bus.
type secretSlot struct {
	mu   sync.Mutex
	open bool
	req  SecretRequest
	ch   chan<- secretReply
	// gen names the prompt in the slot, so a deadline fires only on the
	// prompt that armed it; timer is that prompt's deadline, stopped on
	// answer.
	gen   uint64
	timer *time.Timer
}

func newSecretSlot() *secretSlot { return &secretSlot{} }

// begin claims the slot and names the prompt. It reports false when one is
// already open, having first answered that caller so no request is left
// dangling.
func (s *secretSlot) begin(req SecretRequest, ch chan<- secretReply) (uint64, bool) {
	s.mu.Lock()
	if s.open {
		s.mu.Unlock()
		sendReply(ch, secretReply{noSecrets: true})
		return 0, false
	}
	s.gen++
	s.open, s.req, s.ch = true, req, ch
	gen := s.gen
	s.mu.Unlock()
	return gen, true
}

// arm gives prompt gen its deadline. A prompt already answered stops the
// timer instead of keeping it.
func (s *secretSlot) arm(gen uint64, t *time.Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open || s.gen != gen {
		t.Stop()
		return
	}
	s.timer = t
}

// answer replies exactly once and frees the slot. A second answer is dropped:
// NetworkManager has stopped reading by then.
func (s *secretSlot) answer(r secretReply) {
	s.mu.Lock()
	gen := s.gen
	s.mu.Unlock()
	s.answerGen(gen, r)
}

// answerGen answers prompt gen if it is still open and reports whether it did.
func (s *secretSlot) answerGen(gen uint64, r secretReply) bool {
	s.mu.Lock()
	if !s.open || s.gen != gen {
		s.mu.Unlock()
		return false
	}
	ch := s.ch
	if s.timer != nil {
		s.timer.Stop()
	}
	s.open, s.req, s.ch, s.timer = false, SecretRequest{}, nil, nil
	s.mu.Unlock()
	sendReply(ch, r)
	return true
}

func (s *secretSlot) submit(psk string) { s.answer(secretReply{psk: psk}) }

// cancel answers UserCanceled. Every prompt must reply: a dangling reply hangs
// NetworkManager's activation until its own timeout, which presents to the
// user as a join that silently does nothing.
func (s *secretSlot) cancel() { s.answer(secretReply{cancelled: true}) }

func (s *secretSlot) pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open
}

func (s *secretSlot) request() (SecretRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.req, s.open
}

func sendReply(ch chan<- secretReply, r secretReply) {
	if ch == nil {
		return
	}
	select {
	case ch <- r:
	default:
	}
}

// SecretRequests carries each prompt the panel must show. It is buffered so
// the bus method never waits for the shell relay to be scheduled.
func (n *Network) SecretRequests() <-chan SecretRequest { return n.secretReqs }

// SecretExpiry signals that a shown prompt outlived its deadline and was
// cancelled by the service; the relay clears the password card on it.
func (n *Network) SecretExpiry() <-chan struct{} { return n.secretExpired }

// SubmitSecret answers the open prompt with a passphrase.
//
// The passphrase goes to NetworkManager over the system bus and is stored by
// NetworkManager. It is never written to errLabel, the node tree, a log line,
// or a process argument.
func (n *Network) SubmitSecret(psk string) { n.secrets.submit(psk) }

// CancelSecret answers the open prompt with UserCanceled.
func (n *Network) CancelSecret() { n.secrets.cancel() }

// PendingSecret reports whether a prompt is open, so the panel knows to show
// the password card.
func (n *Network) PendingSecret() (SecretRequest, bool) { return n.secrets.request() }

// startSecrets binds the process-wide export to this service's slot and owns
// its cleanup for the service lifetime.
func (n *Network) startSecrets(start func(*secretExport) (func(), error)) error {
	closeExport, err := start(&secretExport{slot: n.secrets, requests: n.secretReqs, expired: n.secretExpired})
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.closeSecrets = closeExport
	n.mu.Unlock()
	return nil
}

// secretPromptTimeout is the hard deadline for an unanswered password card.
// Without one, GetSecrets holds its D-Bus call and the single slot open until
// NetworkManager cancels -- if it ever does -- blocking every later join
// (GitHub #24, mirroring the Bluetooth pairing timer).
const secretPromptTimeout = 60 * time.Second

// secretExport is the object NetworkManager calls.
type secretExport struct {
	slot     *secretSlot
	requests chan SecretRequest
	// timeout is the prompt deadline; zero selects secretPromptTimeout.
	timeout time.Duration
	// expired nudges the shell relay to clear its password card.
	expired chan struct{}
}

// GetSecrets blocks until the panel answers. godbus runs each incoming call on
// its own goroutine, so blocking here holds only this one method open.
func (e *secretExport) GetSecrets(
	settings map[string]map[string]dbus.Variant,
	connection dbus.ObjectPath,
	settingName string,
	hints []string,
	flags uint32,
) (map[string]map[string]dbus.Variant, *dbus.Error) {
	if settingName != wirelessSecuritySetting {
		return nil, dbus.NewError(errNoSecrets, []any{"only wireless passphrases are answered here"})
	}
	if flags&flagAllowInteraction == 0 {
		// NetworkManager is asking what we already hold, not for a prompt. We
		// hold nothing: it stores the secrets, we only collect them.
		return nil, dbus.NewError(errNoSecrets, []any{"nothing stored"})
	}

	req := SecretRequest{SSID: ssidFromSettings(settings), SettingName: settingName}
	ch := make(chan secretReply, 1)
	gen, ok := e.slot.begin(req, ch)
	if !ok {
		return nil, dbus.NewError(errNoSecrets, []any{"a prompt is already open"})
	}
	select {
	case e.requests <- req:
		e.armDeadline(gen)
	default:
		// A stale notification must not strand this call. Answer once with
		// UserCanceled and let NetworkManager stop.
		e.slot.cancel()
	}

	switch r := <-ch; {
	case r.cancelled:
		return nil, dbus.NewError(errUserCanceled, []any{"cancelled"})
	case r.noSecrets:
		return nil, dbus.NewError(errNoSecrets, []any{"no passphrase given"})
	default:
		return map[string]map[string]dbus.Variant{
			settingName: {"psk": dbus.MakeVariant(r.psk)},
		}, nil
	}
}

// armDeadline cancels prompt gen if it is still unanswered after the timeout,
// and tells the relay. Answering the prompt stops the timer, and a fire that
// races the answer finds another generation, or none, and does nothing.
func (e *secretExport) armDeadline(gen uint64) {
	timeout := e.timeout
	if timeout == 0 {
		timeout = secretPromptTimeout
	}
	e.slot.arm(gen, time.AfterFunc(timeout, func() {
		if !e.slot.answerGen(gen, secretReply{cancelled: true}) {
			return
		}
		if e.expired != nil {
			select {
			case e.expired <- struct{}{}:
			default:
			}
		}
	}))
}

// CancelGetSecrets is NetworkManager withdrawing the question, which must
// still free the slot or the next join can never prompt.
func (e *secretExport) CancelGetSecrets(connection dbus.ObjectPath, settingName string) *dbus.Error {
	e.slot.cancel()
	return nil
}

// SaveSecrets and DeleteSecrets exist because the interface requires them.
// NetworkManager owns the store; this holder keeps nothing.
func (e *secretExport) SaveSecrets(settings map[string]map[string]dbus.Variant, connection dbus.ObjectPath) *dbus.Error {
	return nil
}

func (e *secretExport) DeleteSecrets(settings map[string]map[string]dbus.Variant, connection dbus.ObjectPath) *dbus.Error {
	return nil
}

// ssidFromSettings reads the network's name out of the connection NM is
// activating, so the prompt can say which network it is for.
func ssidFromSettings(settings map[string]map[string]dbus.Variant) string {
	wireless, ok := settings["802-11-wireless"]
	if !ok {
		return ""
	}
	v, ok := wireless["ssid"]
	if !ok {
		return ""
	}
	switch raw := v.Value().(type) {
	case []byte:
		return string(raw)
	case string:
		return raw
	}
	return ""
}

// registerSecretExport publishes the object and files it with NetworkManager.
//
// Registration needs no elevated privilege: it is scoped to this user's
// sessions. Another holder may already be registered -- nm-applet commonly is
// -- and NetworkManager decides which one it asks.
func registerSecretExport(conn *dbus.Conn, e *secretExport) error {
	if conn == nil {
		return fmt.Errorf("services: no system bus")
	}
	if err := conn.Export(e, secretExportPath, secretExportIface); err != nil {
		return fmt.Errorf("services: export secret holder: %w", err)
	}
	obj := conn.Object(networkManagerBus, agentManagerPath)
	if call := obj.Call(agentManagerIface+".Register", 0, secretHolderID); call.Err != nil {
		_ = conn.Export(nil, secretExportPath, secretExportIface)
		return fmt.Errorf("services: register secret holder: %w", call.Err)
	}
	return nil
}

func startSystemSecretExport(e *secretExport) (func(), error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("services: secret holder system bus: %w", err)
	}
	if err := registerSecretExport(conn, e); err != nil {
		return nil, err
	}
	return func() {
		conn.Object(networkManagerBus, agentManagerPath).
			Call(agentManagerIface+".Unregister", 0, secretHolderID)
		_ = conn.Export(nil, secretExportPath, secretExportIface)
	}, nil
}
