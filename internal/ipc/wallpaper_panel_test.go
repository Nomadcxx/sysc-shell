package ipc

import "testing"

func TestKnownPanelsIncludesWallpaper(t *testing.T) {
	if _, ok := knownPanels["wallpaper"]; !ok {
		t.Fatal("the wallpaper panel must be reachable over IPC like the launcher")
	}
}

func TestKnownPanelsIncludesTerminalArt(t *testing.T) {
	if _, ok := knownPanels["terminal-art"]; !ok {
		t.Fatal("the terminal art panel must be reachable over IPC like the wallpaper panel")
	}
}
