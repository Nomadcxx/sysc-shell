package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	ccPrivacyScanAction  = "cc:privacy-scan"
	ccPrivacyReportLimit = 4 << 20
	ccPrivacyScanTimeout = 2 * time.Minute
)

type ccPrivacySavedReport struct {
	Available   bool
	GeneratedAt time.Time
	Grade       string
}

type ccPrivacyScanStatus struct {
	report      *ccPrivacySavedReport
	loading     bool
	scanning    bool
	missing     bool
	unavailable bool
	scanFailed  bool
	generation  uint64
}

func parseCCPrivacySavedReport(raw []byte) (*ccPrivacySavedReport, error) {
	if len(raw) == 0 || len(raw) > ccPrivacyReportLimit {
		return nil, fmt.Errorf("saved privacy report is empty or exceeds %d bytes", ccPrivacyReportLimit)
	}
	var data struct {
		Schema      int       `json:"schema"`
		Available   bool      `json:"available"`
		GeneratedAt time.Time `json:"generated_at"`
		Overall     struct {
			Grade string `json:"grade"`
		} `json:"overall"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse saved privacy report: %w", err)
	}
	if data.Schema != 1 {
		return nil, fmt.Errorf("unsupported saved privacy report schema %d", data.Schema)
	}
	if data.GeneratedAt.IsZero() || !validCCPrivacyGrade(data.Overall.Grade) {
		return nil, errors.New("saved privacy report has an invalid timestamp or grade")
	}
	return &ccPrivacySavedReport{
		Available: data.Available, GeneratedAt: data.GeneratedAt, Grade: data.Overall.Grade,
	}, nil
}

func readCCPrivacySavedReport() (*ccPrivacySavedReport, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome != "" && !filepath.IsAbs(stateHome) {
		return nil, errors.New("XDG_STATE_HOME must be an absolute path")
	}
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(stateHome, "privacybit")
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	dirStat, ok := dirInfo.Sys().(*syscall.Stat_t)
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm()&0o077 != 0 ||
		!ok || dirStat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("privacybit state directory must be private and owned by the current user")
	}
	fd, err := syscall.Open(filepath.Join(dir, "report.json"), syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "report.json")
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("privacybit report must be a private regular file owned by the current user")
	}
	if info.Size() > ccPrivacyReportLimit {
		return nil, errors.New("privacybit report exceeds the storage limit")
	}
	raw, err := io.ReadAll(io.LimitReader(file, ccPrivacyReportLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > ccPrivacyReportLimit {
		return nil, errors.New("privacybit report exceeds the storage limit")
	}
	return parseCCPrivacySavedReport(raw)
}

func ccPrivacySavedReportCard(m theme.Metrics, status ccPrivacyScanStatus, now time.Time) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("Saved report", 0)}
	scanButton := ccSegment("restart_alt", "Run scan", ccPrivacyScanAction, false)
	if status.scanning {
		scanButton.Name = "Scan running"
		scanButton.Children[1].Text = "Scanning…"
		ccDisable(scanButton)
	}
	rows = append(rows, scanButton)
	if status.scanning {
		rows = append(rows, ccPrivacyTextLine("Scan in progress.", ui.ToneActivity))
	}
	if status.scanFailed {
		rows = append(rows, ccPrivacyTextLine("Scan failed. Run privacybit scan in a terminal for details.", ui.ToneError))
	}
	switch {
	case status.loading:
		rows = append(rows, ccPrivacyTextLine("Checking the saved report…", ui.ToneSubtle))
	case status.report == nil && status.unavailable:
		rows = append(rows, ccPrivacyTextLine("The saved report is unavailable or invalid.", ui.ToneSubtle))
	case status.report == nil && status.missing:
		rows = append(rows, ccPrivacyTextLine("No saved report yet.", ui.ToneSubtle))
	case status.report == nil:
		rows = append(rows, ccPrivacyTextLine("No saved report has been checked yet.", ui.ToneSubtle))
	default:
		report := status.report
		age := now.Sub(report.GeneratedAt)
		grade := "unknown"
		if report.Available && age >= 0 && age < ccPrivacyStaleAfter {
			grade = report.Grade
		}
		word, tone := ccPrivacyGradeLabel(grade)
		rows = append(rows,
			ccPrivacyValueRow("Overall", word, tone),
			ccPrivacyValueRow("Scanned", report.GeneratedAt.Local().Format("2 Jan 2006 15:04"), ui.ToneSubtle),
		)
		if age < 0 {
			rows = append(rows, ccPrivacyTextLine("The report timestamp is in the future; its grade is not current.", ui.ToneSubtle))
		} else if age >= ccPrivacyStaleAfter {
			rows = append(rows, ccPrivacyTextLine("The saved report is stale; its grade is not current.", ui.ToneSubtle))
		}
		rows = append(rows, ccPrivacyTextLine("Full details: privacybit report", ui.ToneSubtle))
	}
	return monitorCard(m, rows)
}

// refreshPrivacySavedReportLocked starts a bounded disk read while the caller
// holds Registry.mu. The worker never reads the report on the render path.
func (r *Registry) refreshPrivacySavedReportLocked() {
	if r.privacyScan.scanning {
		return
	}
	r.privacyScan.generation++
	generation := r.privacyScan.generation
	r.privacyScan.report = nil
	r.privacyScan.loading = true
	r.privacyScan.missing = false
	r.privacyScan.unavailable = false
	go func() {
		report, err := readCCPrivacySavedReport()
		r.mu.Lock()
		if r.privacyScan.generation != generation || r.privacyScan.scanning {
			r.mu.Unlock()
			return
		}
		r.privacyScan.loading = false
		r.privacyScan.report = report
		r.privacyScan.missing = errors.Is(err, os.ErrNotExist)
		r.privacyScan.unavailable = err != nil && !r.privacyScan.missing
		panel := r.panelHosts[PanelControlCenter]
		if panel == nil || panel.section != "privacy" {
			r.mu.Unlock()
			return
		}
		r.rebuildPanel(panel)
		output := panel.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelControlCenter))
	}()
}

func (r *Registry) startPrivacyScanLocked(h *PanelHost) {
	if r.privacyScan.scanning {
		return
	}
	r.privacyScan.generation++
	generation := r.privacyScan.generation
	r.privacyScan.loading = false
	r.privacyScan.scanning = true
	r.privacyScan.missing = false
	r.privacyScan.unavailable = false
	r.privacyScan.scanFailed = false
	if r.panelHosts[PanelControlCenter] == h {
		r.rebuildPanel(h)
	}
	go func() {
		report, err := runCCPrivacyScan()
		r.mu.Lock()
		if r.privacyScan.generation != generation {
			r.mu.Unlock()
			return
		}
		r.privacyScan.scanning = false
		r.privacyScan.scanFailed = err != nil
		if err == nil {
			r.privacyScan.report = report
			r.privacyScan.missing = false
			r.privacyScan.unavailable = false
		} else if report == nil && r.privacyScan.report == nil {
			r.privacyScan.unavailable = true
		}
		panel := r.panelHosts[PanelControlCenter]
		if panel == nil || panel.section != "privacy" {
			r.mu.Unlock()
			return
		}
		r.rebuildPanel(panel)
		output := panel.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelControlCenter))
	}()
}

func runCCPrivacyScan() (*ccPrivacySavedReport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ccPrivacyScanTimeout)
	defer cancel()
	executable, err := privacybitExecutable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable, "scan")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("privacybit scan timed out")
		}
		return nil, fmt.Errorf("privacybit scan failed: %w", err)
	}
	report, err := readCCPrivacySavedReport()
	if err != nil {
		return nil, fmt.Errorf("read privacybit report: %w", err)
	}
	return report, nil
}

func privacybitExecutable() (string, error) {
	if path, err := exec.LookPath("privacybit"); err == nil {
		return path, nil
	} else {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			path := filepath.Join(home, ".local", "bin", "privacybit")
			info, statErr := os.Stat(path)
			if statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				return path, nil
			}
			return "", fmt.Errorf("privacybit is not available on PATH or at %s: %w", path, err)
		}
		return "", fmt.Errorf("privacybit is not available on PATH: %w", err)
	}
}
