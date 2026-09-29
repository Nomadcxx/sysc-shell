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
	"strings"
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
		"audio":          "",
		"control-center": "",
		"network":        "",
		"bluetooth":      "",
		"weather":        "",
		"clipboard":      "",
		"plugin-store":   "",
	}
	knownScreenshotModes = map[string]bool{"region": true, "screen": true, "window": true}
)

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
	// Plugins answers every plugins.* method.
	Plugins func(method string, params json.RawMessage) (map[string]any, error)
}

type Server struct {
	path string
	h    Handlers
	ln   net.Listener
}

func NewServer(path string, h Handlers) *Server {
	return &Server{path: path, h: h}
}

func (s *Server) Serve(ctx context.Context) error {
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
	s.ln = ln
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !sameUser(conn) {
			_ = conn.Close()
			continue
		}
		go s.serveConn(conn)
	}
}

// sameUser checks SO_PEERCRED on an accepted connection. The socket's 0600
// mode already restricts who can connect; this catches the window before the
// chmod lands and any socket inherited from an odd setup (GH #8).
func sameUser(conn net.Conn) bool {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return false
	}
	if controlErr != nil {
		return false
	}
	return credential != nil && credential.Uid == uint32(os.Getuid())
}

func (s *Server) Close() error {
	if s.ln == nil {
		return nil
	}
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		resp := s.handleLine(line)
		_, _ = conn.Write(append(resp, '\n'))
	}
}

func (s *Server) handleLine(line string) []byte {
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return envelope(nil, "", "malformed json")
	}
	switch req.Method {
	case "status":
		body := map[string]any{}
		if s.h.Status != nil {
			body = s.h.Status()
		}
		return envelope(req.ID, "ok", "", body)
	case "panel.toggle", "panel.open", "panel.close":
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
	case "osd.step":
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
	case "screenshot":
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
	case "switcher.show":
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
	default:
		if strings.HasPrefix(req.Method, "plugins.") {
			if s.h.Plugins == nil {
				return envelope(req.ID, "", "plugin store handler unset")
			}
			body, err := s.h.Plugins(req.Method, req.Params)
			if err != nil {
				return envelope(req.ID, "", err.Error())
			}
			return envelope(req.ID, "ok", "", body)
		}
		return envelope(req.ID, "", "unknown method")
	}
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

// Call sends one request and returns the raw response line.
func Call(ctx context.Context, sock, method string, params any) (string, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
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
