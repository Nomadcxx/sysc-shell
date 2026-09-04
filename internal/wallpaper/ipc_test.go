package wallpaper

import (
	"bufio"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGSlapper answers one line and then holds the connection open, the way
// gSlapper does. A client that waited for EOF would block until its deadline.
func fakeGSlapper(t *testing.T, reply string) (socket string, received func() string) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "g.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	var mu sync.Mutex
	var got string

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				line, _ := bufio.NewReader(c).ReadString('\n')
				mu.Lock()
				got = line
				mu.Unlock()
				_, _ = io.WriteString(c, reply)
				<-done // never close first
				_ = c.Close()
			}(conn)
		}
	}()
	t.Cleanup(func() {
		close(done)
		_ = listener.Close()
	})
	return socket, func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func TestIPCQuery(t *testing.T) {
	socket, received := fakeGSlapper(t, "STATUS: playing image /wallpapers/space name.png\n")
	reply, err := Request(socket, "query", time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if received() != "query\n" {
		t.Fatalf("wrote %q, want %q", received(), "query\n")
	}
	st, err := ParseStatus(reply)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Paused {
		t.Error("playing must not parse as paused")
	}
	if st.Kind != KindImage {
		t.Errorf("kind = %v, want image", st.Kind)
	}
	if st.Path != "/wallpapers/space name.png" {
		t.Errorf("path = %q, a path with spaces is the fourth field", st.Path)
	}
}

func TestIPCStatusPausedVideo(t *testing.T) {
	st, err := ParseStatus("STATUS: paused video /tmp/a.mp4")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !st.Paused || st.Kind != KindVideo || st.Path != "/tmp/a.mp4" {
		t.Fatalf("got %+v", st)
	}
}

func TestIPCStatusRejects(t *testing.T) {
	for _, line := range []string{"ERROR: no pipeline", "", "STATUS: playing", "nonsense"} {
		if _, err := ParseStatus(line); err == nil {
			t.Errorf("ParseStatus(%q) must fail", line)
		}
	}
	if _, err := ParseStatus("ERROR: no pipeline"); err == nil || !strings.Contains(err.Error(), "no pipeline") {
		t.Errorf("an ERROR reply must carry its message, got %v", err)
	}
}

func TestIPCEmptyReplyIsAnError(t *testing.T) {
	socket, _ := fakeGSlapper(t, "\n")
	if _, err := Request(socket, "query", time.Second); err == nil {
		t.Fatal("an empty reply must be an error")
	}
}

func TestIPCRejectsNewlineBeforeDial(t *testing.T) {
	socket, received := fakeGSlapper(t, "OK\n")
	if _, err := Request(socket, "change /tmp/a.png\nquit", time.Second); err == nil {
		t.Fatal("a command carrying a newline must be refused")
	}
	if received() != "" {
		t.Fatalf("refused command still reached the socket: %q", received())
	}
}

func TestIPCChangeAcceptsOKPrefix(t *testing.T) {
	// Transitions off answer bare OK; transitions on answer OK: transition
	// started. An exact-match accept would break the moment fade is enabled.
	for _, reply := range []string{"OK", "OK: transition started"} {
		if err := checkOK(reply); err != nil {
			t.Errorf("checkOK(%q) = %v, want nil", reply, err)
		}
	}
	if err := checkOK("ERROR: bad path"); err == nil {
		t.Error("an ERROR reply must not read as success")
	}
}

func TestClassifyChangeError(t *testing.T) {
	restart := "ERROR: cannot update path (use --auto-stop for video changes)"
	if classifyChangeError(restart) != changeRestart {
		t.Error("the auto-stop message must classify as restart")
	}
	if classifyChangeError("ERROR: no such file") != changeKeep {
		t.Error("an unrelated error must keep the previous assignment")
	}
}
