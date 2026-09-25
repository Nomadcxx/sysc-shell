package shell

import (
	"context"
	"errors"
	"fmt"
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

func (h *pluginHost) registerWallpaperMask(ctx context.Context, owner string, p v1.WallpaperMaskSetParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.MaskPath == "" {
		h.r.mu.Lock()
		if err := ctx.Err(); err != nil {
			h.r.mu.Unlock()
			return err
		}
		if h.r.depthClocks == nil {
			h.r.mu.Unlock()
			return errors.New("depth clock host is not available")
		}
		effects := h.r.depthClocks.clearLocked(p.Output, owner)
		h.r.mu.Unlock()
		h.r.depthClocks.emit(effects)
		return nil
	}

	mask, err := wallpaper.LoadDepthMask(p.MaskPath, p.WallpaperPath)
	if err != nil {
		return fmt.Errorf("invalid wallpaper depth mask: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner == "" {
		return errors.New("wallpaper depth mask owner is empty")
	}

	h.r.mu.Lock()
	if err := ctx.Err(); err != nil {
		h.r.mu.Unlock()
		return err
	}
	svc := h.r.wallpaperSvc
	if svc == nil {
		h.r.mu.Unlock()
		return errors.New("wallpaper service is not available")
	}
	if h.r.depthClocks == nil {
		h.r.mu.Unlock()
		return errors.New("depth clock host is not available")
	}
	snapshot := svc.Snapshot()
	if !slices.Contains(snapshot.Connectors, p.Output) {
		h.r.mu.Unlock()
		return fmt.Errorf("wallpaper output %q is unavailable", p.Output)
	}
	if _, ok := h.r.outputGlobalsLocked()[p.Output]; !ok {
		h.r.mu.Unlock()
		return fmt.Errorf("wallpaper output %q has no live host", p.Output)
	}
	if snapshot.Runtime[p.Output].State == wallpaper.StateStarting {
		h.r.mu.Unlock()
		return fmt.Errorf("wallpaper output %q is transitioning", p.Output)
	}
	if _, covered := snapshot.Covered[p.Output]; covered {
		h.r.mu.Unlock()
		return fmt.Errorf("wallpaper output %q is covered", p.Output)
	}
	assignment, ok := snapshot.Assignments[p.Output]
	if !ok || assignment.Kind != wallpaper.KindImage || assignment.Path != p.WallpaperPath {
		h.r.mu.Unlock()
		return fmt.Errorf("wallpaper output %q no longer has the requested image", p.Output)
	}
	descriptor := depthClockDescriptor{
		owner: owner, wallpaperPath: p.WallpaperPath, maskPath: p.MaskPath, mask: mask,
	}
	effects, err := h.r.depthClocks.setLocked(p.Output, descriptor)
	h.r.mu.Unlock()
	h.r.depthClocks.emit(effects)
	return err
}
