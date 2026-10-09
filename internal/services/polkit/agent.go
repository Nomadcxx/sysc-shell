// Package polkit registers sysc-shell as the session's polkit authentication
// agent and drives the PAM helper that authenticates a caller.
//
// polkitd routes an authentication request to the agent registered for the
// requesting session. The agent's BeginAuthentication call blocks until the
// user answers or polkitd withdraws the request; the answer itself is
// delivered to polkitd by the PAM helper over a private socket, never over
// this agent's connection. The shell necessarily holds the typed response in
// an immutable Go string while editing; the helper zeroes its byte copy after
// writing it.
package polkit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Policy is the configured behaviour when another agent holds the session.
const (
	// PolicyAuto registers only while no other agent does.
	PolicyAuto = "auto"
	// PolicyOn registers regardless, retrying until the session is free.
	PolicyOn = "on"
	// PolicyOff never registers.
	PolicyOff = "off"
)

// retryInterval is how often PolicyOn retries a registration polkitd refused.
const retryInterval = 30 * time.Second

// busRetryInterval bounds how long registration stays offline after the bus
// disconnects or is unavailable.
const busRetryInterval = 2 * time.Second

// dbusCallTimeout bounds a request to a connected service that stops replying.
const dbusCallTimeout = 5 * time.Second

// ponytail: FAILURE omits the PAM status, so three helper attempts bound retries; use a classified status if the protocol exposes one.
const maxHelperAttempts = 3

// The polkitd wire names. RegisterAuthenticationAgentWithOptions takes no
// reply value, so a registration is one call and its error.
const (
	authorityName  = "org.freedesktop.PolicyKit1"
	authorityPath  = "/org/freedesktop/PolicyKit1/Authority"
	authorityIface = "org.freedesktop.PolicyKit1.Authority"

	agentIface     = "org.freedesktop.PolicyKit1.AuthenticationAgent"
	agentPath      = "/org/sysc/PolicyKit1/AuthenticationAgent"
	registerName   = authorityIface + ".RegisterAuthenticationAgentWithOptions"
	unregisterName = authorityIface + ".UnregisterAuthenticationAgent"

	// alreadyExistsText is polkitd's message when the session already has an
	// agent. Matching on it is the only way to tell that case from a real
	// failure, because both arrive as a plain failed call.
	alreadyExistsText = "An authentication agent already exists for the given subject"

	errCancelled = "org.freedesktop.PolicyKit1.Error.Cancelled"
	errFailed    = "org.freedesktop.PolicyKit1.Error.Failed"
)

// Options configures an Agent. Every seam has a default; tests replace them.
type Options struct {
	// Policy is PolicyAuto, PolicyOn or PolicyOff. Empty means PolicyAuto.
	Policy string
	// Bus dials the system bus. Defaults to dbus.SystemBus.
	Bus func() (*dbus.Conn, error)
	// SessionID overrides the subject's session id.
	SessionID func() (string, error)
	// Helper locates the PAM helper. Empty fields use the polkit defaults.
	Helper HelperSession
	// ProcRoot is where known agents are looked for. Defaults to /proc.
	ProcRoot string
	// Logf receives registration and failure reasons.
	Logf func(format string, args ...any)
}

// Status is what the settings page and the status IPC report.
type Status struct {
	Policy     string `json:"policy"`
	Registered bool   `json:"registered"`
	// Passive names the agent holding the session while this one stands
	// down. Empty unless PolicyAuto lost a registration.
	Passive string `json:"passive,omitempty"`
	// Reason explains why Registered is false.
	Reason string `json:"reason,omitempty"`
}

// CancelledRequest is an authentication request that polkit withdrew or lost
// during an authority restart. It omits the cookie and response state.
type CancelledRequest struct {
	ActionID string
	Details  map[string]string
}

// Agent is the session's authentication agent.
type Agent struct {
	opts    Options
	queue   *queue
	reqs    chan Request
	cancel  chan CancelledRequest
	notice  chan string
	changes chan struct{}

	mu              sync.Mutex
	conn            *dbus.Conn
	status          Status
	registeredOwner string
	held            bool
}

// New builds an agent. Nothing is registered until Run is called.
func New(opts Options) *Agent {
	if opts.Policy == "" {
		opts.Policy = PolicyAuto
	}
	if opts.Bus == nil {
		opts.Bus = func() (*dbus.Conn, error) { return dbus.ConnectSystemBus() }
	}
	if opts.ProcRoot == "" {
		opts.ProcRoot = "/proc"
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	reqs := make(chan Request, 1)
	changes := make(chan struct{}, 1)
	a := &Agent{
		opts:    opts,
		reqs:    reqs,
		cancel:  make(chan CancelledRequest, maxPending+1),
		notice:  make(chan string, 1),
		changes: changes,
	}
	a.queue = &queue{out: reqs, changed: changes}
	a.status = Status{Policy: opts.Policy}
	if opts.Policy == PolicyOff {
		a.status.Reason = "disabled"
	}
	return a
}

// Requests is the displayed request stream. The surface takes one at a time and
// answers it through Request.Start and Request.Submit.
func (a *Agent) Requests() <-chan Request { return a.reqs }

// Changes is a coalesced notification that queue depth or registration status changed.
func (a *Agent) Changes() <-chan struct{} { return a.changes }

// Withdrawn reports requests polkit cancelled, so the shell can tell the user
// which prompt or waiting request disappeared.
func (a *Agent) Withdrawn() <-chan CancelledRequest { return a.cancel }

// Notice reports once that another agent holds the session and is named.
func (a *Agent) Notice() <-chan string { return a.notice }

// Waiting reports how many prompts are queued behind the displayed one.
func (a *Agent) Waiting() int { return a.queue.waiting() }

// Hold defers prompts while the session is locked, so a request waits behind
// the lock screen instead of appearing over it.
func (a *Agent) Hold(locked bool) {
	a.mu.Lock()
	a.held = locked
	a.mu.Unlock()
	a.queue.hold(locked)
}

// Status reports the registration state.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *Agent) setStatus(status Status) {
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()
	select {
	case a.changes <- struct{}{}:
	default:
	}
}

// Run connects, registers, and keeps the registration alive until ctx ends.
// It returns when ctx is done, having unregistered and dropped every prompt.
func (a *Agent) Run(ctx context.Context) error {
	if a.opts.Policy == PolicyOff {
		a.setStatus(Status{Policy: PolicyOff, Reason: "disabled"})
		return nil
	}
	if err := a.opts.Helper.available(); err != nil {
		a.setStatus(Status{Policy: a.opts.Policy, Reason: err.Error()})
		return nil
	}
	for {
		err := a.runConnection(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			err = errors.New("polkit: system bus disconnected")
		}
		a.setStatus(Status{Policy: a.opts.Policy, Reason: err.Error()})
		a.cancelAllRequests()
		a.logf("polkit: connection unavailable, retrying in %s: %v", busRetryInterval, err)
		timer := time.NewTimer(busRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (a *Agent) runConnection(ctx context.Context) error {
	conn, err := a.opts.Bus()
	if err != nil {
		return err
	}
	if conn == nil {
		return errors.New("polkit: system bus returned no connection")
	}
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.conn = nil
		a.registeredOwner = ""
		a.mu.Unlock()
		_ = conn.Close()
	}()

	if err := conn.Export(a, agentPath, agentIface); err != nil {
		return err
	}
	defer conn.Export(nil, agentPath, agentIface)

	sid := ""
	if a.opts.SessionID != nil {
		if sid, err = a.opts.SessionID(); err != nil {
			return err
		}
	} else if sid, err = sessionID(ctx, conn); err != nil {
		return err
	}

	owner := make(chan *dbus.Signal, 8)
	conn.Signal(owner)
	match := []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, authorityName),
	}
	matchCtx, cancelMatch := context.WithTimeout(ctx, dbusCallTimeout)
	err = conn.AddMatchSignalContext(matchCtx, match...)
	cancelMatch()
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), dbusCallTimeout)
		defer cancel()
		_ = conn.RemoveMatchSignalContext(cleanupCtx, match...)
	}()
	defer conn.RemoveSignal(owner)

	// A retry timer only for PolicyOn; nil means the select waits on ctx only.
	var retry <-chan time.Time
	if a.opts.Policy == PolicyOn {
		ticker := time.NewTicker(retryInterval)
		defer ticker.Stop()
		retry = ticker.C
	}
	for {
		if err := a.register(ctx, conn, sid); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			a.unregister(conn, sid)
			a.queue.cancelAll()
			return nil
		case sig, ok := <-owner:
			if !ok {
				return errors.New("polkit: system bus disconnected")
			}
			// polkitd restarting drops every registration it held. The
			// prompts it had queued are gone with it, so their sessions must
			// not wait for an answer that can no longer arrive.
			if sig != nil && sig.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(sig.Body) == 3 {
				name, nameOK := sig.Body[0].(string)
				oldOwner, oldOK := sig.Body[1].(string)
				newOwner, newOK := sig.Body[2].(string)
				if nameOK && name == authorityName && oldOK && newOK && oldOwner != newOwner {
					a.handleAuthorityOwnerChange(oldOwner, newOwner)
				}
			}
		case <-retry:
		}
	}
}

func (a *Agent) cancelAllRequests() {
	for _, request := range a.queue.cancelAll() {
		a.cancel <- CancelledRequest{ActionID: request.ActionID, Details: request.Details}
	}
}

func (a *Agent) handleAuthorityOwnerChange(oldOwner, newOwner string) bool {
	if oldOwner == newOwner {
		return false
	}
	// Signals already queued when registration succeeds can describe the
	// change that led to this registered owner, not a later authority restart.
	// Only a change from the owner we registered with invalidates the state.
	a.mu.Lock()
	if a.status.Registered && a.registeredOwner != "" && oldOwner != a.registeredOwner {
		a.mu.Unlock()
		return false
	}
	a.registeredOwner = ""
	a.mu.Unlock()
	a.setStatus(Status{Policy: a.opts.Policy})
	a.cancelAllRequests()
	return true
}

// register tries unless this connection already registered successfully.
func (a *Agent) register(ctx context.Context, conn *dbus.Conn, sid string) error {
	if a.Status().Registered {
		return nil
	}
	owner, err := authorityOwner(ctx, conn)
	if isNameHasNoOwner(err) {
		// GetNameOwner does not activate a service. Start polkitd explicitly so
		// the registration call can target the unique owner we just discovered.
		err = callWithTimeout(ctx, conn.BusObject(), "org.freedesktop.DBus.StartServiceByName", authorityName, uint32(0)).Err
		if err == nil {
			owner, err = authorityOwner(ctx, conn)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		// Pin the call to the unique owner from GetNameOwner. If that process
		// loses the well-known name while handling the request, the queued
		// NameOwnerChanged signal invalidates this exact registration below.
		err = callWithTimeout(ctx, conn.Object(owner, authorityPath), registerName,
			subject(sid), locale(), agentPath, map[string]dbus.Variant{}).Err
	}
	if ctx.Err() != nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case err == nil:
		a.mu.Lock()
		a.registeredOwner = owner
		a.mu.Unlock()
		a.setStatus(Status{Policy: a.opts.Policy, Registered: true})
		a.logf("polkit: registered as the authentication agent for session %s", sid)
	case strings.Contains(err.Error(), alreadyExistsText):
		holder := nameExisting(a.opts.ProcRoot)
		if holder == "" {
			holder = "an unknown authentication agent"
		}
		reason := holder + " holds this session"
		previous := a.Status()
		a.setStatus(Status{Policy: a.opts.Policy, Passive: holder, Reason: reason})
		a.logf("polkit: standing down, %s", reason)
		if previous.Reason != reason {
			select {
			case a.notice <- holder:
			default:
			}
		}
	case a.opts.Policy == PolicyAuto:
		// auto does not compete: a failure leaves the shell as it was.
		a.setStatus(Status{Policy: a.opts.Policy, Reason: err.Error()})
		a.logf("polkit: registration refused: %v", err)
	default:
		a.setStatus(Status{Policy: a.opts.Policy, Reason: err.Error()})
		a.logf("polkit: registration refused, retrying in %s: %v", retryInterval, err)
	}
	return nil
}

func authorityOwner(ctx context.Context, conn *dbus.Conn) (string, error) {
	var owner string
	err := callWithTimeout(ctx, conn.BusObject(), "org.freedesktop.DBus.GetNameOwner", authorityName).Store(&owner)
	if err == nil && owner == "" {
		return "", errors.New("polkit: authority has no bus owner")
	}
	return owner, err
}

func callWithTimeout(ctx context.Context, object dbus.BusObject, method string, args ...any) *dbus.Call {
	callCtx, cancel := context.WithTimeout(ctx, dbusCallTimeout)
	defer cancel()
	return object.CallWithContext(callCtx, method, 0, args...)
}

func isNameHasNoOwner(err error) bool {
	var busErr dbus.Error
	return errors.As(err, &busErr) && busErr.Name == "org.freedesktop.DBus.Error.NameHasNoOwner"
}

func (a *Agent) unregister(conn *dbus.Conn, sid string) {
	a.mu.Lock()
	owner := a.registeredOwner
	a.mu.Unlock()
	destination := authorityName
	if owner != "" {
		destination = owner
	}
	if err := callWithTimeout(context.Background(), conn.Object(destination, authorityPath), unregisterName, subject(sid), agentPath).Err; err != nil {
		a.logf("polkit: unregister: %v", err)
	}
	a.mu.Lock()
	a.registeredOwner = ""
	a.mu.Unlock()
	a.setStatus(Status{Policy: a.opts.Policy, Reason: "stopped"})
}

// Interface names the D-Bus interface this object exports.
func (a *Agent) Interface() string { return agentIface }

// BeginAuthentication blocks until the request is answered or withdrawn.
//
// godbus dispatches every call on its own goroutine, so a blocked prompt does
// not stall CancelAuthentication; that is how polkitd reaches a waiting
// BeginAuthentication to end it.
func (a *Agent) BeginAuthentication(sender dbus.Sender, actionID, message, iconName string, details map[string]string, cookie string, identities []polkitIdentity) *dbus.Error {
	if !a.authorized(sender) {
		return dbus.NewError(errFailed, []any{"caller is not polkitd"})
	}
	if !validRequest(actionID, message, iconName, details, cookie, identities) {
		return dbus.NewError(errFailed, []any{"invalid authentication request"})
	}
	req := newRequest()
	req.ActionID, req.Message, req.IconName = actionID, message, iconName
	req.Details, req.Cookie = details, cookie
	for _, pair := range identities {
		kind := pair.Kind
		identity := Identity{Kind: kind, Values: map[string]string{}}
		for k, v := range pair.Values {
			identity.Values[k] = fmt.Sprint(v.Value())
		}
		if identity.Kind == "unix-user" {
			identity.Name = usernameOf(identity.Values["uid"])
		}
		req.Identities = append(req.Identities, identity)
	}

	_, err := a.queue.push(req)
	if err != nil {
		if errors.Is(err, errQueueCancelled) {
			return dbus.NewError(errCancelled, []any{err.Error()})
		}
		return dbus.NewError(errFailed, []any{err.Error()})
	}
	defer func() {
		req.done.stop()
		a.queue.finish(req.Cookie)
	}()
	return a.run(req)
}

// polkitIdentity is the `(sa{sv})` entry in BeginAuthentication's identity
// array. Field order is the D-Bus struct order.
type polkitIdentity struct {
	Kind   string
	Values map[string]dbus.Variant
}

// run drives the helper session for a displayed request. It is the goroutine
// BeginAuthentication is already running in, which is what makes the whole
// prompt a blocking D-Bus call.
func (a *Agent) run(req Request) *dbus.Error {
	var selected Identity
	select {
	case selected = <-req.identity:
	case <-req.Done():
		return dbus.NewError(errCancelled, []any{"request withdrawn"})
	}
	username := ""
	for _, offered := range req.Identities {
		if offered.Kind == selected.Kind && offered.Values["uid"] == selected.Values["uid"] && offered.Kind == "unix-user" {
			username = usernameOf(offered.Values["uid"])
			break
		}
	}
	if username == "" {
		return dbus.NewError(errFailed, []any{"no valid unix-user identity selected"})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-req.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	attempts := 0
	for {
		outcome, err := a.session(req, username).Run(ctx, req.ask)
		switch {
		case err == nil && outcome == tagFailure:
			attempts++
			if attempts >= maxHelperAttempts {
				a.logf("polkit: authentication failed after %d helper attempts", attempts)
				return dbus.NewError(errFailed, []any{"authentication failed"})
			}
			a.logf("polkit: authentication attempt failed")
			if _, askErr := req.ask(Prompt{Text: "Authentication failed. Try again."}); askErr != nil {
				return dbus.NewError(errCancelled, []any{"request withdrawn"})
			}
		case err == nil:
			a.logf("polkit: authentication request finished: %s", outcome)
			return nil
		case err == ErrHelperCancelled:
			return dbus.NewError(errCancelled, []any{"request withdrawn"})
		default:
			a.logf("polkit: authentication request failed: %v", err)
			return dbus.NewError(errFailed, []any{err.Error()})
		}
	}
}

// session is the helper conversation for one request. The user name is the
// PAM user, which polkitd derives from the identity it offers.
func (a *Agent) session(req Request, username string) HelperSession {
	s := a.opts.Helper
	s.SocketPath, s.BinaryPath = a.opts.Helper.SocketPath, a.opts.Helper.BinaryPath
	s.Cookie = req.Cookie
	s.Username = username
	return s
}

// CancelAuthentication ends a request polkitd no longer wants. The caller died,
// or the authorisation was revoked.
func (a *Agent) CancelAuthentication(sender dbus.Sender, cookie string) *dbus.Error {
	if !a.authorized(sender) {
		return dbus.NewError(errFailed, []any{"caller is not polkitd"})
	}
	if cookie == "" || len(cookie) > 256 {
		return dbus.NewError(errFailed, []any{"invalid authentication cookie"})
	}
	withdrawn, ok := a.queue.withdraw(cookie)
	if !ok {
		// Not one of ours. polkitd only cancels what it asked us for, so this
		// is a duplicate call rather than an error worth surfacing.
		a.logf("polkit: cancel for unknown authentication request")
		return nil
	}
	if withdrawn.queued {
		a.cancel <- CancelledRequest{ActionID: withdrawn.request.ActionID, Details: withdrawn.request.Details}
	}
	return nil
}

func (a *Agent) authorized(sender dbus.Sender) bool {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil || sender == "" {
		return false
	}
	owner, err := authorityOwner(context.Background(), conn)
	if err != nil {
		return false
	}
	return string(sender) == owner
}

func validRequest(actionID, message, iconName string, details map[string]string, cookie string, identities []polkitIdentity) bool {
	if actionID == "" || len(actionID) > 256 || len(message) > 8192 || len(iconName) > 256 ||
		cookie == "" || len(cookie) > 256 || len(details) > 64 || len(identities) == 0 || len(identities) > 32 {
		return false
	}
	for key, value := range details {
		if key == "" || len(key) > 128 || len(value) > 2048 {
			return false
		}
	}
	for _, identity := range identities {
		if identity.Kind == "" || len(identity.Kind) > 128 || len(identity.Values) > 32 {
			return false
		}
		for key, value := range identity.Values {
			if key == "" || len(key) > 128 || len(fmt.Sprint(value.Value())) > 256 {
				return false
			}
		}
	}
	return true
}

func usernameOf(uid string) string {
	if uid == "" {
		return ""
	}
	entry, err := user.LookupId(uid)
	if err != nil {
		return ""
	}
	return entry.Username
}

func locale() string {
	if l := os.Getenv("LANG"); l != "" {
		return l
	}
	return "en_US.UTF-8"
}

func (a *Agent) logf(format string, args ...any) { a.opts.Logf("polkit: "+format, args...) }
