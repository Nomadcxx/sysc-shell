package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const socketName = "ipc.v1.sock"

var (
	ErrSingleInstance = errors.New("sysc-shell is already running")
	knownPanels       = map[string]string{
		"clock":          "",
		"system-monitor": "",
		"session":        "",
		"power":          "",
		"settings":       "",
		"launcher":       "",
		"notifications":  "",
		"wallpaper":      "",
		"terminal-art":   "",
		"audio":          "",
		"control-center": "",
		"network":        "",
		"bluetooth":      "",
		"weather":        "",
		"clipboard":      "",
		"plugin-store":   "",
		"files":          "",
		"onboarding":     "",
	}
	knownScreenshotModes = map[string]bool{"region": true, "screen": true, "window": true}
)

// PanelIDs lists the panel vocabulary in a stable order. The documentation
// gate and the panel dispatcher have to agree on the same set; exporting it
// lets a test assert they do, rather than an example drifting until it names a
// panel the shell refuses.
func PanelIDs() []string {
	out := make([]string, 0, len(knownPanels))
	for id := range knownPanels {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// DefaultSocket returns $XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock.
// os.TempDir() is world-writable and shared between users; the XDG base spec's
// own fallback (/run/user/UID) keeps the socket inside our private directory.
func DefaultSocket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "sysc-shell", socketName)
}

type Handlers struct {
	Panel    func(action, panel, section string) error
	OSDStep  func(kind, action string) error
	Status   func() map[string]any
	Switcher func() error
	// Screenshot starts a capture in one of knownScreenshotModes. It returns
	// once the capture has started; the result reaches the user as a toast,
	// because a region waits on the user and Call's deadline cannot.
	Screenshot func(mode string) error
	// LockState answers session.lock-state: {running, acquired,
	// respawned, exit_code} for the tracked session locker (sysc-lock
	// handshake semantics; third-party lockers report running only).
	LockState func() map[string]any
	// Plugins answers every plugins.* method.
	Plugins func(method string, params json.RawMessage) (map[string]any, error)
	// Theme answers every theme.* method.
	Theme func(method string, params json.RawMessage) (map[string]any, error)
	// NightLight answers every nightlight.* method.
	NightLight func(method string, params json.RawMessage) (map[string]any, error)
	// API holds the API-2 methods, keyed by exact name. A method is listed in
	// capabilities exactly when its entry is non-nil.
	API map[string]APIMethod
	// Topics holds the subscribable state topics, keyed by name.
	Topics map[string]Provider
}

// shutdownGrace bounds how long Serve waits for in-flight requests once its
// context is cancelled. A handler stuck on a lock must not hold shutdown.
const shutdownGrace = 2 * time.Second

// writeTimeout bounds one response write, so a client that stops reading
// cannot pin its connection goroutine.
const writeTimeout = 5 * time.Second

type Server struct {
	path string
	h    Handlers
	ln   net.Listener
	hubs map[string]*hub
	// writeTimeout bounds one write; tests shorten it.
	writeTimeout time.Duration
	// peerUID reports the uid of the peer on an accepted connection. It is a
	// field so a test can present a peer from another user, which a
	// same-uid process cannot do on its own.
	peerUID func(net.Conn) (uint32, bool)

	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	consumers int
	wg        sync.WaitGroup
}

func NewServer(path string, h Handlers) *Server {
	s := &Server{path: path, writeTimeout: writeTimeout, peerUID: peerCredentials}
	s.SetHandlers(h)
	return s
}

// Listen binds the socket and enforces single-instance. The production order
// is Listen before any service starts, so a duplicate launch fails before it
// has spawned a plugin or claimed a bus name. Connections that arrive before
// Serve starts wait in the listen backlog. Serve calls Listen itself when it
// has not run.
func (s *Server) Listen() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		if !isAddrInUse(err) {
			return err
		}
		probe, dialErr := net.DialTimeout("unix", s.path, 200*time.Millisecond)
		if dialErr == nil {
			_ = probe.Close()
			return ErrSingleInstance
		}
		_ = os.Remove(s.path)
		ln, err = net.Listen("unix", s.path)
		if err != nil {
			return err
		}
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	s.setListener(ln)
	return nil
}

// listener returns the bound listener, if Listen has run. Close can run while
// Serve is still inside Listen, so the field is shared between those
// goroutines and every read and write of it goes through s.mu.
func (s *Server) listener() net.Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ln
}

// setListener records the bound listener. The mutex release publishes the
// listener together with every field net.Listen wrote while building it, so a
// concurrent Close either sees a fully initialised listener or none at all.
func (s *Server) setListener(ln net.Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ln = ln
}

// SetHandlers installs the request handlers. It must run before Serve: the
// accept loop is what serves the backlog, so every connection is handled by
// the handlers installed here.
func (s *Server) SetHandlers(h Handlers) {
	s.h = h
	s.hubs = map[string]*hub{}
	for name, p := range h.Topics {
		if p.Start != nil {
			s.hubs[name] = &hub{name: name, provider: p}
		}
	}
}

func (s *Server) Serve(ctx context.Context) error {
	ln := s.listener()
	if ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
		ln = s.listener()
	}
	if ln == nil {
		return ErrSingleInstance
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.drain()
				return nil
			}
			return err
		}
		if !s.sameUser(conn) {
			_ = conn.Close()
			continue
		}
		s.track(conn)
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = map[net.Conn]struct{}{}
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
}

// drain ends every open connection and waits, within shutdownGrace, for the
// request each one is running. Serve returns only after this, so the caller can
// tear down what the handlers use knowing no new request starts (gh #74).
func (s *Server) drain() {
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
	}
}

// sameUser checks SO_PEERCRED on an accepted connection. The socket's 0600
// mode already restricts who can connect; this catches the window before the
// chmod lands and any socket inherited from an odd setup (GH #8).
func (s *Server) sameUser(conn net.Conn) bool {
	lookup := s.peerUID
	if lookup == nil {
		lookup = peerCredentials
	}
	uid, ok := lookup(conn)
	if !ok {
		return false
	}
	return sameUID(uid, uint32(os.Getuid()))
}

// sameUID reports whether the peer runs as the same user as the shell. A peer
// whose uid cannot be read is not the same user: the gate denies it.
func sameUID(peer, self uint32) bool {
	return peer == self
}

// peerCredentials reads the uid of the peer on an accepted connection with
// SO_PEERCRED. It reports false when the peer credentials are unavailable.
func peerCredentials(conn net.Conn) (uint32, bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, false
	}
	if controlErr != nil || credential == nil {
		return 0, false
	}
	return credential.Uid, true
}

func (s *Server) Close() error {
	ln := s.listener()
	if ln == nil {
		return nil
	}
	err := ln.Close()
	_ = os.Remove(s.path)
	return err
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	c := newSession(s, conn)
	defer func() {
		c.close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	sc := bufio.NewScanner(conn)
	// One byte over the limit, so a line of exactly lineBytes still fits
	// beside its newline and anything longer is ErrTooLong.
	sc.Buffer(make([]byte, 4096), lineBytes+1)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if resp := s.dispatch(c, line); resp != nil {
			c.reply(resp)
		}
	}
	if errors.Is(sc.Err(), bufio.ErrTooLong) {
		c.reply(codedError(nil, CodeLimitExceeded, fmt.Sprintf("request line over %d bytes", lineBytes)))
	}
}

// methods is the whole exact-method vocabulary. Dispatching from it, rather
// than from a switch, means the documentation gate and the socket read the same
// list: an example naming a method the dispatcher does not carry fails, and a
// new method is added in one place.
var methods = map[string]func(*Server, request) []byte{
	"status": func(s *Server, req request) []byte {
		body := map[string]any{}
		if s.h.Status != nil {
			body = s.h.Status()
		}
		return envelope(req.ID, "ok", "", body)
	},
	"session.lock-state": func(s *Server, req request) []byte {
		if s.h.LockState == nil {
			return envelope(req.ID, "", "lock state handler unset")
		}
		return envelope(req.ID, "ok", "", s.h.LockState())
	},
	"panel.toggle": panelMethod,
	"panel.open":   panelMethod,
	"panel.close":  panelMethod,
	"osd.step": func(s *Server, req request) []byte {
		var params struct {
			Kind   string `json:"kind"`
			Action string `json:"action"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return envelope(req.ID, "", "malformed params")
			}
		}
		if s.h.OSDStep == nil {
			return envelope(req.ID, "", "osd handler unset")
		}
		if err := s.h.OSDStep(params.Kind, params.Action); err != nil {
			return envelope(req.ID, "", err.Error())
		}
		return envelope(req.ID, "ok", "")
	},
	"screenshot": func(s *Server, req request) []byte {
		var params struct {
			Mode string `json:"mode"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return envelope(req.ID, "", "malformed params")
			}
		}
		if !knownScreenshotModes[params.Mode] {
			return envelope(req.ID, "", "unknown screenshot mode")
		}
		if s.h.Screenshot == nil {
			return envelope(req.ID, "", "screenshot handler unset")
		}
		if err := s.h.Screenshot(params.Mode); err != nil {
			return envelope(req.ID, "", err.Error())
		}
		return envelope(req.ID, "ok", "")
	},
	"switcher.show": func(s *Server, req request) []byte {
		if len(req.Params) > 0 {
			var params struct{}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return envelope(req.ID, "", "malformed params")
			}
		}
		if s.h.Switcher == nil {
			return envelope(req.ID, "", "switcher handler unset")
		}
		if err := s.h.Switcher(); err != nil {
			return envelope(req.ID, "", err.Error())
		}
		return envelope(req.ID, "ok", "")
	},
}

func panelMethod(s *Server, req request) []byte {
	action := strings.TrimPrefix(req.Method, "panel.")
	var params struct {
		Panel   string `json:"panel"`
		Section string `json:"section"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return envelope(req.ID, "", "malformed params")
		}
	}
	msg, ok := knownPanels[params.Panel]
	if !ok {
		return envelope(req.ID, "", "unknown panel")
	}
	if msg != "" {
		return envelope(req.ID, "", msg)
	}
	if s.h.Panel == nil {
		return envelope(req.ID, "", "panel handler unset")
	}
	if err := s.h.Panel(action, params.Panel, params.Section); err != nil {
		return envelope(req.ID, "", err.Error())
	}
	return envelope(req.ID, "ok", "")
}

// delegatedFamily is a method prefix one handler owns whole.
type delegatedFamily struct {
	prefix string
	unset  string
	owner  func(*Handlers) delegated
}

var delegatedFamilies = []delegatedFamily{
	{"plugins.", "plugin store handler unset", func(h *Handlers) delegated { return h.Plugins }},
	{"theme.", "theme handler unset", func(h *Handlers) delegated { return h.Theme }},
	{"nightlight.", "night light handler unset", func(h *Handlers) delegated { return h.NightLight }},
}

// delegated is the shape every family handler has: the whole method name and
// its raw params, or a body and an error.
type delegated func(string, json.RawMessage) (map[string]any, error)

// Methods lists the exact methods every server dispatches, legacy and
// built-in API-2, in a stable order. Handlers.API entries are per server and
// appear in that server's capabilities.
func Methods() []string {
	out := make([]string, 0, len(methods)+len(builtinAPI))
	for name := range methods {
		out = append(out, name)
	}
	out = append(out, builtinAPI...)
	sort.Strings(out)
	return out
}

// MethodPrefixes lists the delegated method families in a stable order. The
// dispatch order inside delegatedFamilies is its own concern and is not
// exported.
func MethodPrefixes() []string {
	out := make([]string, 0, len(delegatedFamilies))
	for _, fam := range delegatedFamilies {
		out = append(out, fam.prefix)
	}
	sort.Strings(out)
	return out
}

// handleLine answers one request outside any connection, so ipc.subscribe
// cannot succeed through it.
func (s *Server) handleLine(line string) []byte {
	return s.dispatch(nil, line)
}

// dispatch answers one request line on a connection. A nil reply means the
// session has already queued its own.
func (s *Server) dispatch(c *session, line string) []byte {
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return codedError(nil, CodeInvalidParams, "malformed json")
	}
	switch req.Method {
	case "ipc.subscribe":
		return c.subscribe(req)
	case "ipc.unsubscribe":
		return c.unsubscribe(req)
	}
	if c.isEvents() {
		return codedError(req.ID, CodeUnsupported, "this connection carries events; send commands on another connection")
	}
	if req.Method == "ipc.capabilities" {
		return callAPI(req.ID, s.capabilities, req.Params)
	}
	if fn, ok := methods[req.Method]; ok {
		return fn(s, req)
	}
	if fn := s.h.API[req.Method]; fn != nil {
		return callAPI(req.ID, fn, req.Params)
	}
	for _, fam := range delegatedFamilies {
		if !strings.HasPrefix(req.Method, fam.prefix) {
			continue
		}
		owner := fam.owner(&s.h)
		if owner == nil {
			return envelope(req.ID, "", fam.unset)
		}
		body, err := owner(req.Method, req.Params)
		if err != nil {
			return envelope(req.ID, "", err.Error())
		}
		return envelope(req.ID, "ok", "", body)
	}
	return codedError(req.ID, CodeUnsupported, "unknown method")
}

func envelope(id json.RawMessage, ok, err string, extra ...map[string]any) []byte {
	m := map[string]any{"id": json.RawMessage("null")}
	if len(id) > 0 {
		m["id"] = id
	}
	if err != "" {
		m["error"] = err
	} else {
		m["ok"] = true
		if len(extra) > 0 {
			for k, v := range extra[0] {
				m[k] = v
			}
		}
	}
	b, _ := json.Marshal(m)
	return b
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use")
}

func callTimeout(method string) time.Duration {
	switch method {
	case "theme.preview.show":
		// Matugen itself is bounded at ten seconds.
		return 12 * time.Second
	case "theme.palettes.save":
		// Two generations (dark and light), each bounded at ten seconds.
		return 25 * time.Second
	}
	return 2 * time.Second
}

// Call sends one request and returns the raw response line.
func Call(ctx context.Context, sock, method string, params any) (string, error) {
	timeout := callTimeout(method)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(callCtx, "unix", sock)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline, _ := callCtx.Deadline()
	_ = conn.SetDeadline(deadline)
	req := map[string]any{"id": 1, "method": method, "params": params}
	if params == nil {
		req["params"] = map[string]any{}
	}
	b, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no response")
	}
	return sc.Text(), nil
}
