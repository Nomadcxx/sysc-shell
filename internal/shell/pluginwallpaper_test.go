package shell

import (
	"context"
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestPluginWallpaperSnapshotNeedsService(t *testing.T) {
	h := &pluginHost{r: &Registry{}}
	if _, err := h.wallpaperSnapshot(context.Background()); err == nil || err.Error() != "wallpaper service is not available" {
		t.Fatalf("snapshot error = %v, want wallpaper service unavailable", err)
	}
}

func TestGrantedCapabilitiesIncludeWallpaper(t *testing.T) {
	for _, capability := range hostPluginCaps {
		if capability == plugin.CapWallpaper {
			return
		}
	}
	t.Fatal("host plugin capabilities omit wallpaper")
}

func TestPluginWallpaperSnapshotProjectsOutputs(t *testing.T) {
	projection := &pluginWallpaperProjection{}
	snapshot := wallpaper.Snapshot{
		Connectors: []string{"DP-5", "DP-4", "DP-3", "DP-2", "DP-1"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/wallpaper.jpg"},
			"DP-2": {Kind: wallpaper.KindVideo, Path: "/wallpaper.mp4"},
			"DP-4": {Kind: wallpaper.KindImage, Path: "/transition.jpg"},
			"DP-5": {Kind: wallpaper.KindImage, Path: "/covered.jpg"},
		},
		Runtime: map[string]wallpaper.Runtime{
			"DP-4": {State: wallpaper.StateStarting},
			"DP-5": {State: wallpaper.StateStarting},
		},
		Covered: map[string]string{"DP-5": "foreign-background"},
	}

	got := projection.project(snapshot, "fill")
	want := []v1.WallpaperOutput{
		{Output: "DP-1", State: v1.WallpaperImage, Path: "/wallpaper.jpg"},
		{Output: "DP-2", State: v1.WallpaperVideo},
		{Output: "DP-3", State: v1.WallpaperNone},
		{Output: "DP-4", State: v1.WallpaperTransitioning},
		{Output: "DP-5", State: v1.WallpaperCovered},
	}
	if !reflect.DeepEqual(got.Outputs, want) {
		t.Fatalf("outputs = %+v, want %+v", got.Outputs, want)
	}
	if got.Scale != "fill" || got.Revision != 1 {
		t.Fatalf("projection = %+v, want scale fill and initial revision 1", got)
	}
}

func TestPluginWallpaperSnapshotRevisionTracksVisibleChanges(t *testing.T) {
	projection := &pluginWallpaperProjection{}
	snapshot := wallpaper.Snapshot{
		Connectors: []string{"DP-1"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/first.jpg"},
		},
	}

	first := projection.project(snapshot, "fill")
	if got := projection.project(snapshot, "fill").Revision; got != first.Revision {
		t.Fatalf("unchanged revision = %d, want %d", got, first.Revision)
	}

	snapshot.Assignments["DP-1"] = wallpaper.Assignment{Kind: wallpaper.KindImage, Path: "/second.jpg"}
	pathChange := projection.project(snapshot, "fill")
	if pathChange.Revision != first.Revision+1 {
		t.Fatalf("path change revision = %d, want %d", pathChange.Revision, first.Revision+1)
	}

	snapshot.Runtime = map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StateStarting}}
	stateChange := projection.project(snapshot, "fill")
	if stateChange.Revision != pathChange.Revision+1 {
		t.Fatalf("state change revision = %d, want %d", stateChange.Revision, pathChange.Revision+1)
	}

	scaleChange := projection.project(snapshot, "stretch")
	if scaleChange.Revision != stateChange.Revision+1 {
		t.Fatalf("scale change revision = %d, want %d", scaleChange.Revision, stateChange.Revision+1)
	}
}
