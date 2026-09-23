package shell

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

type pluginWallpaperProjection struct {
	mu       sync.Mutex
	revision uint64
	last     string
}

func (p *pluginWallpaperProjection) project(snapshot wallpaper.Snapshot, scale string) v1.WallpaperSnapshotResult {
	connectors := slices.Clone(snapshot.Connectors)
	slices.Sort(connectors)
	outputs := make([]v1.WallpaperOutput, 0, len(connectors))
	for _, connector := range connectors {
		out := v1.WallpaperOutput{Output: connector, State: v1.WallpaperNone}
		_, isCovered := snapshot.Covered[connector]
		switch {
		case isCovered:
			out.State = v1.WallpaperCovered
		case snapshot.Runtime[connector].State == wallpaper.StateStarting:
			out.State = v1.WallpaperTransitioning
		default:
			assignment, ok := snapshot.Assignments[connector]
			if ok {
				switch assignment.Kind {
				case wallpaper.KindImage:
					out.State, out.Path = v1.WallpaperImage, assignment.Path
				case wallpaper.KindVideo:
					out.State = v1.WallpaperVideo
				}
			}
		}
		outputs = append(outputs, out)
	}

	fingerprint := wallpaperFingerprint(scale, outputs)
	p.mu.Lock()
	if fingerprint != p.last {
		p.revision++
		p.last = fingerprint
	}
	result := v1.WallpaperSnapshotResult{Revision: p.revision, Scale: scale, Outputs: outputs}
	p.mu.Unlock()
	return result
}

func wallpaperFingerprint(scale string, outputs []v1.WallpaperOutput) string {
	var b strings.Builder
	write := func(value string) {
		b.WriteString(strconv.Quote(value))
		b.WriteByte(0)
	}
	write(scale)
	for _, output := range outputs {
		write(output.Output)
		write(string(output.State))
		write(output.Path)
	}
	return b.String()
}

func (h *pluginHost) wallpaperSnapshot(context.Context) (v1.WallpaperSnapshotResult, error) {
	h.r.mu.Lock()
	svc := h.r.wallpaperSvc
	scale := h.r.cfg.Wallpaper.Scale
	h.r.mu.Unlock()
	if svc == nil {
		return v1.WallpaperSnapshotResult{}, errors.New("wallpaper service is not available")
	}
	return h.wallpaperProjection.project(svc.Snapshot(), scale), nil
}
