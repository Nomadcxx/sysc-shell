package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	panelWallpaperAction = "panel:wallpaper"
	// wallpaperGlyph is a Unicode symbol rather than an entry in the embedded
	// icon subset. Adding one there means re-cutting the Material subset from
	// the pinned upstream font, whose checksum the builder verifies precisely
	// so a substituted file cannot produce plausible but wrong shapes. That is
	// an authoring-time step with the real source file, not something to force
	// here, so the widget draws from the text stack until it happens.
	wallpaperGlyph = "▦"
)

// buildWallpaperWidget is the bar affordance for the wallpaper picker.
//
// config.knownItems has accepted "wallpaper" since the picker landed, with a
// comment saying a user who wants the glyph adds it. buildWidgets had no case
// for it, so adding it to a bar validated at load and then produced nothing.
// The glyph never changes, but a widget still has to write its state through
// format: applyLocked calls it for every widget without a refresh, so a nil
// one is not "nothing to update", it is a nil call on the first bar apply.
func buildWallpaperWidget() textWidget {
	return textWidget{
		node: &ui.Node{
			Kind:     ui.KindText,
			Text:     wallpaperGlyph,
			TextRole: theme.RoleLabel,
			Action:   panelWallpaperAction,
		},
		tooltip: "Wallpaper",
		format:  func(barView) string { return wallpaperGlyph },
	}
}
