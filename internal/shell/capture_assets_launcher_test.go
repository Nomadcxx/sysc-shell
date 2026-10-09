package shell

import (
	"context"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// assetApps is the application list the launcher shows: ordinary programs, none
// of them read from the machine the screenshot is taken on.
func assetApps() []launcher.Entry {
	app := func(id, name, generic, icon string) launcher.Entry {
		return launcher.Entry{ID: id + ".desktop", Name: name, GenericName: generic, Comment: generic, IconName: icon, Argv: []string{id}}
	}
	return []launcher.Entry{
		app("browser", "Browser", "Web Browser", "web-browser"),
		app("calculator", "Calculator", "Calculator", "accessories-calculator"),
		app("calendar", "Calendar", "Calendar", "office-calendar"),
		app("files", "Files", "File Manager", "system-file-manager"),
		app("imageviewer", "Image Viewer", "Image Viewer", "image-x-generic"),
		app("mail", "Mail", "Email Client", "mail-client"),
		app("musicplayer", "Music Player", "Audio Player", "multimedia-audio-player"),
		app("notes", "Notes", "Note Taking", "accessories-text-editor"),
		app("settings", "Settings", "System Settings", "preferences-system"),
		app("terminal", "Terminal", "Terminal Emulator", "utilities-terminal"),
		app("texteditor", "Text Editor", "Text Editor", "accessories-text-editor"),
		app("videoplayer", "Video Player", "Video Player", "video-x-generic"),
	}
}

func TestAssetLauncher(t *testing.T) {
	assetsDir(t)
	for _, tc := range []struct{ name, query string }{{"browse", ""}, {"query", "te"}} {
		t.Run(tc.name, func(t *testing.T) {
			reg, _, reqs := openLauncherPanel(t, assetApps())
			reg.mu.Lock()
			assetBase(t, reg)
			// The icon worker resolves each entry's icon name against the installed theme.
			worker := icons.NewWorker(icons.NewResolver("", nil), nil)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			go func() { _ = worker.Run(ctx) }()
			reg.trayIcons = worker
			reg.mu.Unlock()
			want := len(assetApps())
			if tc.query != "" {
				reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: tc.query})
				waitForLauncherState(t, reg, func(h *PanelHost) bool {
					return h != nil && !h.launcherAwaiting && len(h.launcherResults) > 0 && len(h.launcherResults) < len(assetApps())
				})
				want = -1
			}
			if want >= 0 {
				waitForLauncherResults(t, reg, want)
			}
			// The first paint asks the icon worker for each icon; the second shows them.
			paintAssetPanel(t, reg, PanelLauncher, reqs[1].Open, "launcher", tc.name)
			time.Sleep(600 * time.Millisecond)
			paintAssetPanel(t, reg, PanelLauncher, reqs[1].Open, "launcher", tc.name)
		})
	}
}
