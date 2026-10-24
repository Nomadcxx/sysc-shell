package shell

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A plugin whose view the host refuses must learn why. The operator reads the
// journal line and the plugin author reads the refusal itself: a blank panel
// with no explanation is the failure this covers.
func TestPluginHostRefusesAViewAndSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
		want string
	}{
		{name: "wrong root kind", mode: "bad-view", want: "needs a row root"},
		{name: "past the node budget", mode: "too-many-nodes", want: "more than 1024 nodes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "view-error.txt")
			t.Setenv("SYSC_HELPER_VIEW_ERROR", record)
			sink := capturePluginLogs(t)

			reg := bindTestPlugin(t, tc.mode)
			newHosts(t, reg, map[uint32]string{7: "DP-1"})

			refusal := waitViewRefusal(t, record)
			if !strings.Contains(refusal, tc.want) {
				t.Fatalf("refusal %q does not name the rule %q", refusal, tc.want)
			}

			rec, ok := sink.find("plugin view rejected")
			if !ok {
				t.Fatal("the host logged no refusal")
			}
			if got := attrString(rec, "plugin"); got != "org.sysc.timer" {
				t.Errorf("logged plugin = %q, want org.sysc.timer", got)
			}
			if got := attrString(rec, "view"); got != "bar" {
				t.Errorf("logged view = %q, want bar", got)
			}
			if got := attrString(rec, "err"); !strings.Contains(got, tc.want) {
				t.Errorf("logged reason %q does not name the rule %q", got, tc.want)
			}
		})
	}
}

// waitViewRefusal reads the refusal the plugin recorded, waiting for it.
func waitViewRefusal(t testing.TB, path string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the plugin never learned that its view was refused")
	return ""
}

// pluginLogSink keeps the journal lines a test wants to read.
type pluginLogSink struct {
	mu      sync.Mutex
	records []slog.Record
}

func (s *pluginLogSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *pluginLogSink) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r.Clone())
	return nil
}

func (s *pluginLogSink) WithAttrs([]slog.Attr) slog.Handler { return s }

func (s *pluginLogSink) WithGroup(string) slog.Handler { return s }

// find returns the first record with the given message.
func (s *pluginLogSink) find(message string) (slog.Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.Message == message {
			return rec, true
		}
	}
	return slog.Record{}, false
}

// capturePluginLogs makes the journal readable by the test.
func capturePluginLogs(t *testing.T) *pluginLogSink {
	t.Helper()
	sink := &pluginLogSink{}
	previous := slog.Default()
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return sink
}

// attrString reads one attribute of a journal record.
func attrString(rec slog.Record, key string) string {
	var out string
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value.String()
			return false
		}
		return true
	})
	return out
}
