package ipc

import "testing"

func TestKnownPanelsIncludesWallpaper(t *testing.T) {
	if _, ok := knownPanels["wallpaper"]; !ok {
		t.Fatal("the wallpaper panel must be reachable over IPC like the launcher")
	}
}
