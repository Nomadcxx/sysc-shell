package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const savedPrivacyReportFixture = `{"schema":1,"available":true,"generated_at":"2026-10-20T12:00:00Z","overall":{"grade":"amber"}}`

func TestParseCCPrivacySavedReport(t *testing.T) {
	report, err := parseCCPrivacySavedReport([]byte(savedPrivacyReportFixture))
	if err != nil {
		t.Fatal(err)
	}
	if report.Grade != "amber" || !report.GeneratedAt.Equal(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("saved report = %+v", report)
	}

	cases := map[string]string{
		"malformed":     "{",
		"future schema": strings.Replace(savedPrivacyReportFixture, `"schema":1`, `"schema":2`, 1),
		"invalid grade": strings.Replace(savedPrivacyReportFixture, `"grade":"amber"`, `"grade":"excellent"`, 1),
		"missing time":  strings.Replace(savedPrivacyReportFixture, `,"generated_at":"2026-10-20T12:00:00Z"`, "", 1),
		"oversized":     strings.Repeat("x", ccPrivacyReportLimit+1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCCPrivacySavedReport([]byte(raw)); err == nil {
				t.Fatal("invalid saved report was accepted")
			}
		})
	}
}

func TestReadCCPrivacySavedReportRequiresPrivateRegularFile(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := filepath.Join(stateHome, "privacybit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, []byte(savedPrivacyReportFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if report, err := readCCPrivacySavedReport(); err != nil || report == nil || report.Grade != "amber" {
		t.Fatalf("read private report = %+v, error = %v", report, err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCCPrivacySavedReport(); err == nil {
		t.Fatal("reader accepted a group-readable report")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(external, []byte(savedPrivacyReportFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if _, err := readCCPrivacySavedReport(); err == nil {
		t.Fatal("reader accepted a report symlink")
	}
}

func TestPrivacySavedReportCardShowsCurrentGradeAndScanAction(t *testing.T) {
	now := time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC)
	r := &Registry{now: now}
	r.privacyScan.report = &ccPrivacySavedReport{
		Available: true, GeneratedAt: now.Add(-time.Minute), Grade: "amber",
	}
	h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
	page := ccPrivacy(r, h)
	if button := findAction(page, ccPrivacyScanAction); button == nil || button.Name != "Run scan" {
		t.Fatalf("scan action = %+v", button)
	}
	text := renderText(page)
	for _, want := range []string{"Saved report", "Review", "Scanned", "Run scan", "privacybit report"} {
		if !strings.Contains(text, want) {
			t.Errorf("privacy page lost %q: %q", want, text)
		}
	}
}

func TestPrivacySavedReportCardMakesStaleAndFutureGradesUnrated(t *testing.T) {
	generated := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{"stale", generated.Add(15 * time.Minute)},
		{"future", generated.Add(-time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{now: tc.now}
			r.privacyScan.report = &ccPrivacySavedReport{Available: true, GeneratedAt: generated, Grade: "red"}
			h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
			text := renderText(ccPrivacy(r, h))
			if !strings.Contains(text, "Unrated") || strings.Contains(text, "Risky") {
				t.Fatalf("stale/future report presented a current grade: %q", text)
			}
		})
	}
}

func TestEnteringPrivacyReadsTimerCreatedReport(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := filepath.Join(stateHome, "privacybit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(savedPrivacyReportFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	h := r.panelHosts[PanelControlCenter]
	r.mu.Lock()
	h.selectControlCentreSection(r, "privacy")
	r.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		loading, report := r.privacyScan.loading, r.privacyScan.report
		r.mu.Unlock()
		if !loading && report != nil {
			if report.Grade != "amber" {
				t.Fatalf("timer-created report grade = %q", report.Grade)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Privacy page did not load the saved timer report")
}

func TestPrivacyScanActionRunsOnceOffRegistryLockAndRefreshesReport(t *testing.T) {
	stateHome, startedPath, releasePath := startBlockedPrivacybit(t)
	generated := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r := &Registry{now: generated.Add(time.Minute)}
	h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
	activate := func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.activateControlCentre(r, &ui.Node{Action: ccPrivacyScanAction})
	}

	activated := make(chan bool, 1)
	r.mu.Lock()
	go func() {
		activated <- h.activateControlCentre(r, &ui.Node{Action: ccPrivacyScanAction})
		r.mu.Unlock()
	}()
	select {
	case ok := <-activated:
		if !ok {
			t.Fatal("Run scan action was not handled")
		}
	case <-time.After(time.Second):
		_ = os.WriteFile(releasePath, nil, 0o600)
		<-activated
		t.Fatal("Run scan blocked the Control Centre lock")
	}
	waitForFile(t, startedPath)
	r.mu.Lock()
	busyPage := ccPrivacy(r, h)
	r.mu.Unlock()
	if findAction(busyPage, ccPrivacyScanAction) != nil || !strings.Contains(renderText(busyPage), "Scan in progress") {
		t.Fatalf("in-flight scan did not disable its action and show status: %q", renderText(busyPage))
	}
	if !activate() {
		t.Fatal("duplicate Run scan action was not handled")
	}
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForPrivacyScan(t, r)

	r.mu.Lock()
	got := r.privacyScan.report
	scanFailed := r.privacyScan.scanFailed
	r.mu.Unlock()
	if got == nil || got.Grade != "amber" || !got.GeneratedAt.Equal(generated) || scanFailed {
		t.Fatalf("scan result = %+v, failed=%v", got, scanFailed)
	}
	args, err := os.ReadFile(filepath.Join(stateHome, "privacybit", "args"))
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "scan\n" {
		t.Fatalf("privacybit arguments = %q, want one scan invocation", args)
	}
}

func TestPrivacyScanActionShowsMissingExecutableFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
	r.mu.Lock()
	if !h.activateControlCentre(r, &ui.Node{Action: ccPrivacyScanAction}) {
		r.mu.Unlock()
		t.Fatal("Run scan action was not handled")
	}
	r.mu.Unlock()
	waitForPrivacyScan(t, r)

	r.mu.Lock()
	failed := r.privacyScan.scanFailed
	text := renderText(ccPrivacy(r, h))
	r.mu.Unlock()
	if !failed || !strings.Contains(text, "Scan failed") {
		t.Fatalf("missing privacybit did not produce a visible scan failure: %q", text)
	}
}

func startBlockedPrivacybit(t *testing.T) (stateHome, startedPath, releasePath string) {
	t.Helper()
	stateHome = t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	releasePath = filepath.Join(stateHome, "release-scan")
	startedPath = filepath.Join(stateHome, "scan-started")
	if err := os.Mkdir(filepath.Join(stateHome, "privacybit"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("PRIVACYBIT_TEST_STARTED", startedPath)
	t.Setenv("PRIVACYBIT_TEST_RELEASE", releasePath)
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$XDG_STATE_HOME/privacybit/args\"\n" +
		": > \"$PRIVACYBIT_TEST_STARTED\"\n" +
		"while [ ! -e \"$PRIVACYBIT_TEST_RELEASE\" ]; do :; done\n" +
		"umask 077\n" +
		"printf '%s\\n' '" + savedPrivacyReportFixture + "' > \"$XDG_STATE_HOME/privacybit/report.json\"\n"
	if err := os.WriteFile(filepath.Join(bin, "privacybit"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(releasePath, nil, 0o600) })
	return stateHome, startedPath, releasePath
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForPrivacyScan(t *testing.T, r *Registry) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		busy := r.privacyScan.scanning
		r.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("privacy scan did not finish")
}
