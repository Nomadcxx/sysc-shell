package wallpaper

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
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
	// The command is a line in a line protocol, so an embedded newline would
	// smuggle a second command past the caller. This is checked before dialling
	// so a malformed command never reaches a running engine.
	if strings.ContainsAny(command, "\n\r") {
		return "", fmt.Errorf("wallpaper: command %q contains a newline", command)
	}
	conn, err := net.DialTimeout("unix", socket, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()

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
