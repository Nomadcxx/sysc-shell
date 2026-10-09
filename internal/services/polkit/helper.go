package polkit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// The helper paths polkit 127 uses. The socket is the path on any install
// with polkit-agent-helper.socket running, which is every current one: the
// helper reads the caller's uid from SO_PEERPIDFD, so it needs no privilege
// and the agent needs no setuid bit. The binary is the fallback for an install
// that still has the setuid helper.
const (
	defaultHelperSocket  = "/run/polkit/agent-helper.socket"
	defaultHelperBinary  = "/usr/lib/polkit-1/polkit-agent-helper-1"
	helperMaxResponseLen = 512 // PAM_MAX_RESP_SIZE, the helper's line buffer
)

// Helper tags. polkitagenthelperprivate.h defines these as the whole protocol.
const (
	tagEchoOff  = "PAM_PROMPT_ECHO_OFF"
	tagEchoOn   = "PAM_PROMPT_ECHO_ON"
	tagErrorMsg = "PAM_ERROR_MSG"
	tagTextInfo = "PAM_TEXT_INFO"
	tagSuccess  = "SUCCESS"
	tagFailure  = "FAILURE"
)

var (
	// ErrHelperCancelled means the session ended because the request was
	// withdrawn, not because the helper finished.
	ErrHelperCancelled = errors.New("polkit: helper session cancelled")
	// ErrNoHelper means neither helper path is available.
	ErrNoHelper = errors.New("polkit authentication helper not found")
)

// HelperSession is one conversation with the PAM helper.
type HelperSession struct {
	// SocketPath and BinaryPath override the helper locations, which is how
	// tests point the runner at a fake helper.
	SocketPath string
	BinaryPath string
	Username   string
	Cookie     string
}

// Prompt is one message from the helper.
type Prompt struct {
	Text string
	// Secret marks a PAM_PROMPT_ECHO_* line, which needs an answer. Echo is
	// true for PAM_PROMPT_ECHO_ON, so the surface can leave that response
	// visible; PAM_ERROR_MSG and PAM_TEXT_INFO need no answer.
	Secret bool
	Echo   bool
}

// Run drives one helper session to its end.
//
// ask is called for every line the helper sends. A secret prompt returns the
// user's answer; a display prompt returns "" and only shows text. The helper
// answers polkitd itself, so the only thing Run reports is how the session
// ended.
//
// Cancelling ctx closes the connection or kills the process; Run then returns
// ErrHelperCancelled, which is how a withdrawn request ends its session.
func (s HelperSession) Run(ctx context.Context, ask func(Prompt) (string, error)) (outcome string, runErr error) {
	defer func() {
		if runErr != nil && ctx.Err() != nil {
			outcome, runErr = "", ErrHelperCancelled
		}
	}()
	socket, binary := s.SocketPath, s.BinaryPath
	if socket == "" {
		socket = defaultHelperSocket
	}
	if binary == "" {
		binary = defaultHelperBinary
	}

	var (
		in         io.WriteCloser
		out        io.ReadCloser
		cancel     func()
		socketMode bool
	)
	switch {
	case isHelperSocket(socket):
		conn, dialErr := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if dialErr != nil {
			return "", fmt.Errorf("polkit: dial helper socket: %w", dialErr)
		}
		in, out, cancel = conn, conn, func() { _ = conn.Close() }
		socketMode = true
	case helperBinaryAvailable(binary, s.BinaryPath != ""):
		cmd := exec.Command(binary, s.Username)
		stdin, pipeErr := cmd.StdinPipe()
		if pipeErr != nil {
			return "", fmt.Errorf("polkit: helper stdin: %w", pipeErr)
		}
		stdout, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			return "", fmt.Errorf("polkit: helper stdout: %w", pipeErr)
		}
		if pipeErr := cmd.Start(); pipeErr != nil {
			return "", fmt.Errorf("polkit: start helper: %w", pipeErr)
		}
		in, out, cancel = stdin, stdout, func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	default:
		return "", ErrNoHelper
	}

	// One closer for both paths: the socket reads and writes over the same
	// connection, and killing the process must also drop its stdin.
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(cancel) }
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()

	// The socket helper gets its username on stdin. The setuid helper already
	// got it as argv[1] and reads only the cookie from stdin.
	if socketMode {
		if werr := writeLine(in, s.Username); werr != nil {
			return "", werr
		}
	}
	if werr := writeLine(in, s.Cookie); werr != nil {
		return "", werr
	}

	reader := bufio.NewReader(out)
	for {
		line, rerr := reader.ReadString('\n')
		if rerr != nil {
			if ctx.Err() != nil {
				return "", ErrHelperCancelled
			}
			if len(strings.TrimSpace(line)) == 0 {
				return "", fmt.Errorf("polkit: helper exited without an answer")
			}
		}
		// g_strcompress: the helper escapes every message it sends.
		text := unescapeHelper(strings.TrimRight(line, "\r\n"))
		tag, message, _ := strings.Cut(text, " ")
		switch {
		case tag == tagSuccess:
			return tagSuccess, nil
		case tag == tagFailure:
			return tagFailure, nil
		case tag == tagEchoOff || tag == tagEchoOn:
			answer, aerr := ask(Prompt{Text: message, Secret: true, Echo: tag == tagEchoOn})
			if aerr != nil {
				// The user dismissed the prompt; the helper waits for a line
				// it will never get, so the session is over either way.
				return "", aerr
			}
			if werr := writeResponse(in, answer); werr != nil {
				return "", werr
			}
		case tag == tagErrorMsg || tag == tagTextInfo:
			// Display only. The surface shows it and answers nothing.
			if _, aerr := ask(Prompt{Text: message}); aerr != nil {
				return "", aerr
			}
		default:
			return "", fmt.Errorf("polkit: unexpected helper line %q", tag)
		}
		if rerr != nil {
			return "", fmt.Errorf("polkit: helper read: %w", rerr)
		}
	}
}

func (s HelperSession) available() error {
	socket, binary := s.SocketPath, s.BinaryPath
	if socket == "" {
		socket = defaultHelperSocket
	}
	if binary == "" {
		binary = defaultHelperBinary
	}
	if isHelperSocket(socket) || helperBinaryAvailable(binary, s.BinaryPath != "") {
		return nil
	}
	return ErrNoHelper
}

// writeLine sends one protocol line. It is only ever the username or the
// cookie, neither of which is a secret.
func writeLine(w io.Writer, line string) error {
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		return fmt.Errorf("polkit: helper write: %w", err)
	}
	return nil
}

// writeResponse sends the user's answer as a raw line: the helper does not
// escape what it reads back, and truncating at PAM_MAX_RESP_SIZE is what it
// does with a longer line anyway.
//
// The bytes are zeroed once written. The answer came from a field, and the
// copy in this frame is the one that outlives the surface.
func writeResponse(w io.Writer, answer string) error {
	buf := []byte(answer)
	defer zero(buf)
	if len(buf) > helperMaxResponseLen-1 {
		buf = buf[:helperMaxResponseLen-1]
	}
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("polkit: helper write: %w", err)
	}
	return writeLine(w, "")
}

// zero overwrites b. The compiler keeps the backing array alive here, so this
// is what actually clears the copy of a password in this process.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// unescapeHelper reverses g_strcompress, which is how the helper escapes every
// message: \a \b \f \n \r \t \v \\ \" and three-digit octal.
func unescapeHelper(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			out.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'a':
			out.WriteByte('\a')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'v':
			out.WriteByte('\v')
		case '\\':
			out.WriteByte('\\')
		case '"':
			out.WriteByte('"')
		default:
			if s[i] >= '0' && s[i] <= '7' && i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i:i+3], 8, 8); err == nil {
					out.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

func isHelperSocket(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

func helperBinaryAvailable(path string, allowNonSetuid bool) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return allowNonSetuid || info.Mode()&os.ModeSetuid != 0
}
