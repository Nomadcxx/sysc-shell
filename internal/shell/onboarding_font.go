package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/render"
)

type onboardingFontState uint8

const (
	onboardingFontOff onboardingFontState = iota
	onboardingFontChecking
	onboardingFontCovered
	onboardingFontMissing
	onboardingFontUnavailable
	onboardingFontInstalling
	onboardingFontInstalled
	onboardingFontFailed
)

func cjkFontLanguage(locale string) string {
	switch locale {
	case "ja":
		return "ja"
	case "ko":
		return "ko"
	default:
		return ""
	}
}

func cjkFontLanguageName(locale string) string {
	switch locale {
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	default:
		return "CJK"
	}
}

func hasCJKFontCoverage(language string) bool {
	var sample string
	switch language {
	case "ja":
		sample = "日本語"
	case "ko":
		sample = "한국어"
	default:
		return false
	}
	fonts, err := render.NewSystemFontMap("sans-serif", "")
	if err != nil {
		return false
	}
	for _, glyph := range sample {
		if !fonts.HasGlyph(glyph) {
			return false
		}
	}
	return true
}

type cjkFontCommandSpec struct {
	manager string
	args    []string
}

// cjkFontCommandForOS supports only distro IDs with verified Noto package names.
// ponytail: exact IDs cover Arch, Debian, Ubuntu, and Fedora; add a derivative only after checking its package repositories.
func cjkFontCommandForOS(osRelease []byte) (cjkFontCommandSpec, bool) {
	id := ""
	for _, line := range strings.Split(string(osRelease), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key != "ID" {
			continue
		}
		id = strings.ToLower(strings.Trim(value, `"'`))
		break
	}
	switch id {
	case "arch":
		return cjkFontCommandSpec{manager: "/usr/bin/pacman", args: []string{"-S", "--needed", "--noconfirm", "--", "noto-fonts-cjk"}}, true
	case "debian", "ubuntu":
		return cjkFontCommandSpec{manager: "/usr/bin/apt-get", args: []string{"install", "-y", "--", "fonts-noto-cjk"}}, true
	case "fedora":
		return cjkFontCommandSpec{manager: "/usr/bin/dnf", args: []string{"install", "-y", "--", "google-noto-sans-cjk-ttc-fonts"}}, true
	default:
		return cjkFontCommandSpec{}, false
	}
}

func cjkFontInstallAvailable() bool {
	if os.Geteuid() == 0 || !executableFile("/usr/bin/pkexec") {
		return false
	}
	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	spec, ok := cjkFontCommandForOS(osRelease)
	return ok && executableFile(spec.manager)
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func runCJKFontInstall() error {
	if os.Geteuid() == 0 {
		return errors.New("refusing a package install from a root shell")
	}
	if !executableFile("/usr/bin/pkexec") {
		return errors.New("pkexec is unavailable")
	}
	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return fmt.Errorf("read os-release: %w", err)
	}
	spec, ok := cjkFontCommandForOS(osRelease)
	if !ok {
		return errors.New("unsupported distribution")
	}
	if !executableFile(spec.manager) {
		return errors.New("system package manager is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	args := append([]string{spec.manager}, spec.args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/pkexec", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("package install failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func onboardingFontCopy(locale string, state onboardingFontState) string {
	language := cjkFontLanguageName(locale)
	switch state {
	case onboardingFontChecking:
		return "Checking installed fonts for " + language + "…"
	case onboardingFontCovered:
		return "Installed fonts already cover " + language + ". No extra package is needed."
	case onboardingFontMissing:
		return "No installed fonts cover " + language + ". Noto CJK can add the missing glyphs system-wide."
	case onboardingFontUnavailable:
		return "No installed fonts cover " + language + ". Automatic installation is unavailable on this system; use its package manager to add a CJK font."
	case onboardingFontInstalling:
		return "Installing Noto CJK. Approve the request in the system authorization prompt."
	case onboardingFontInstalled:
		return "Noto CJK is installed. Restart sysc-shell to use the new fonts."
	case onboardingFontFailed:
		return "The font install did not complete. Check the authorization prompt or package manager, then retry."
	default:
		return ""
	}
}

// checkOnboardingFont runs fontconfig and package-manager availability probes
// off Registry.mu. Callers hold the lock and rebuild their current page.
func (r *Registry) checkOnboardingFont(h *PanelHost) {
	locale := i18n.Resolve(h.draft.Session.Language)
	language := cjkFontLanguage(locale)
	if language == "" {
		h.onbFontLocale, h.onbFontState = "", onboardingFontOff
		return
	}
	if h.onbFontLocale == locale && h.onbFontState != onboardingFontOff {
		return
	}
	h.onbFontLocale = locale
	switch {
	case r.onboardingFontInstalled:
		h.onbFontState = onboardingFontInstalled
		return
	case r.onboardingFontInstallRunning:
		h.onbFontState = onboardingFontInstalling
		return
	default:
		h.onbFontState = onboardingFontChecking
	}
	go func() {
		covered := r.onboardingFontCoverage(language)
		available := !covered && r.onboardingFontInstallAvailable()
		r.mu.Lock()
		if r.panelHosts[PanelOnboarding] != h || h.onbFontLocale != locale {
			r.mu.Unlock()
			return
		}
		switch {
		case r.onboardingFontInstalled:
			h.onbFontState = onboardingFontInstalled
		case r.onboardingFontInstallRunning:
			h.onbFontState = onboardingFontInstalling
		case covered:
			h.onbFontState = onboardingFontCovered
		case available:
			h.onbFontState = onboardingFontMissing
		default:
			h.onbFontState = onboardingFontUnavailable
		}
		r.rebuildPanel(h)
		output := h.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelOnboarding))
	}()
}

// installOnboardingFont is reachable only from the explicit Install button.
// Registry state survives a panel close so a running install cannot be
// duplicated and a completed install keeps its restart guidance until relaunch.
func (r *Registry) installOnboardingFont(h *PanelHost) {
	if r.onboardingFontInstalled {
		h.onbFontState = onboardingFontInstalled
		r.rebuildPanel(h)
		return
	}
	if r.onboardingFontInstallRunning {
		h.onbFontState = onboardingFontInstalling
		r.rebuildPanel(h)
		return
	}
	if h.onbFontState != onboardingFontMissing && h.onbFontState != onboardingFontFailed {
		return
	}
	locale := h.onbFontLocale
	if cjkFontLanguage(locale) == "" {
		return
	}
	r.onboardingFontInstallRunning = true
	h.onbFontState = onboardingFontInstalling
	go func() {
		err := r.onboardingFontInstall()
		if err != nil {
			log.Printf("shell: CJK font install: %v", err)
		}
		r.mu.Lock()
		r.onboardingFontInstallRunning = false
		if err == nil {
			r.onboardingFontInstalled = true
		}
		current := r.panelHosts[PanelOnboarding]
		if current == nil || cjkFontLanguage(current.onbFontLocale) == "" {
			r.mu.Unlock()
			return
		}
		if err != nil {
			current.onbFontState = onboardingFontFailed
		} else {
			current.onbFontState = onboardingFontInstalled
		}
		r.rebuildPanel(current)
		output := current.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelOnboarding))
	}()
}
