package wallpaper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Timeouts for one IPC round trip. Pause and resume are slower than the rest
// because they touch a running GStreamer pipeline.
const (
	ipcTimeout         = 2 * time.Second
	ipcPlaybackTimeout = 6 * time.Second
)

// Status is one parsed `query` reply.
type Status struct {
	Paused bool
	// Kind is what gSlapper says it is playing, which is not always what the
	// library classified: gSlapper treats GIF as an image. The active strip
	// follows this field so a file never shows a control its pipeline cannot
	// honour.
	Kind Kind
	Path string
}

// Request sends one command on an owned socket and returns the single reply
// line.
//
// The reply is read a line at a time rather than to EOF: gSlapper keeps the
// connection open after answering, so reading to EOF would block until the
// deadline on every successful call.
func Request(socket, command string, timeout time.Duration) (string, error) {
	return requestWithPeer(context.Background(), socket, command, timeout, 0)
}

// requestWithPeer sends one command after checking that the path is a real
// socket and that the connected server has the expected credentials. A peer
// PID of zero means that only the effective UID is required; the engine passes
// its owned process PID, which closes the path-replacement window between
// checking a socket and sending a lifecycle command.
func requestWithPeer(ctx context.Context, socket, command string, timeout time.Duration, peerPID int) (string, error) {
	// The command is a line in a line protocol, so an embedded newline would
	// smuggle a second command past the caller. This is checked before dialling
	// so a malformed command never reaches a running engine.
	if strings.ContainsAny(command, "\n\r") {
		return "", fmt.Errorf("wallpaper: command %q contains a newline", command)
	}
	info, err := os.Lstat(socket)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("wallpaper: %s is not a unix socket", socket)
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return "", fmt.Errorf("wallpaper: %s is not a unix socket", socket)
	}
	connectionDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-connectionDone:
		}
	}()
	defer close(connectionDone)
	credential, err := peerCredentials(unixConn)
	if err != nil {
		return "", fmt.Errorf("wallpaper: read peer credentials: %w", err)
	}
	uid := uint32(os.Geteuid())
	if credential.Uid != uid {
		return "", fmt.Errorf("wallpaper: %s is served by uid %d, want %d", socket, credential.Uid, uid)
	}
	if peerPID > 0 && int(credential.Pid) != peerPID {
		return "", fmt.Errorf("wallpaper: %s is served by pid %d, want %d", socket, credential.Pid, peerPID)
	}

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte(command + "\n")); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	// A reply without a trailing newline is still a reply if the engine then
	// held the connection; only an empty read is a failure.
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("wallpaper: empty reply")
	}
	return line, nil
}

func peerCredentials(conn *net.UnixConn) (*unix.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	if controlErr != nil {
		return nil, controlErr
	}
	if credential == nil {
		return nil, errors.New("wallpaper: peer credentials were empty")
	}
	return credential, nil
}

// socketPeerPID is used only while recording a socket that never reached the
// ready protocol. It gives cleanup the same process identity proof as normal
// IPC without sending a command.
func socketPeerPID(socket string) (int, error) {
	conn, err := net.DialTimeout("unix", socket, ipcTimeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("wallpaper: not a unix socket")
	}
	credential, err := peerCredentials(unixConn)
	if err != nil {
		return 0, err
	}
	return int(credential.Pid), nil
}

// ParseStatus reads a `query` reply.
//
// The path is the fourth field and may contain spaces, so the line is split
// into exactly four parts and the remainder is taken whole.
func ParseStatus(line string) (Status, error) {
	if err := checkOK(line); err != nil {
		return Status{}, err
	}
	fields := strings.SplitN(line, " ", 4)
	if len(fields) != 4 || fields[0] != "STATUS:" {
		return Status{}, fmt.Errorf("wallpaper: unparsable status %q", line)
	}
	var st Status
	switch fields[1] {
	case "playing":
	case "paused":
		st.Paused = true
	default:
		return Status{}, fmt.Errorf("wallpaper: unknown playback state %q", fields[1])
	}
	switch fields[2] {
	case "image":
		st.Kind = KindImage
	case "video":
		st.Kind = KindVideo
	default:
		return Status{}, fmt.Errorf("wallpaper: unknown media kind %q", fields[2])
	}
	st.Path = fields[3]
	return st, nil
}

// checkOK turns an engine reply into an error, and accepts success on the OK
// prefix: transitions off answer a bare `OK`, transitions on answer
// `OK: transition started`.
func checkOK(reply string) error {
	if rest, found := strings.CutPrefix(reply, "ERROR:"); found {
		return fmt.Errorf("wallpaper: gslapper:%s", rest)
	}
	if reply == "" {
		return errors.New("wallpaper: empty reply")
	}
	return nil
}

// changeOutcome says what to do after a `change` was refused.
type changeOutcome uint8

const (
	// changeKeep leaves the previous assignment in place and shows the error.
	// Relaunching on an arbitrary failure would loop against a bad file.
	changeKeep changeOutcome = iota
	// changeRestart stops our instance and launches a new one.
	changeRestart
)

// autoStopMessage is what gSlapper answers when it is asked to change a video
// path without --auto-stop. It is the one error worth restarting for.
const autoStopMessage = "use --auto-stop for video changes"

func classifyChangeError(reply string) changeOutcome {
	if strings.Contains(reply, autoStopMessage) {
		return changeRestart
	}
	return changeKeep
}
