package wallpaper

import (
	"errors"
	"fmt"
	"strings"
)

type EffectInfo struct {
	ID   string
	Text bool
}

type Catalog struct {
	Effects []EffectInfo
	Themes  []string
}

func ParseList(s string) Catalog {
	var c Catalog
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "effect":
			c.Effects = append(c.Effects, EffectInfo{ID: f[1], Text: len(f) >= 3 && f[2] != "0"})
		case "theme":
			c.Themes = append(c.Themes, f[1])
		}
	}
	return c
}

func terminalArgs(socket, connector, effect, theme, artwork string) []string {
	args := []string{"sysc-terminal", "-I", socket, "--output", connector, "--effect", effect}
	if theme != "" {
		args = append(args, "--theme", theme)
	}
	if artwork != "" {
		args = append(args, "--file", artwork)
	}
	return args
}

func (e *gslapperEngine) applyEffect(job Job, set Settings) (string, error) {
	unlock := e.lockConnector(job.Connector)
	defer unlock()
	if e.isClosed() {
		return "", errEngineClosed
	}
	if !e.generationCurrent(job) {
		return "", nil
	}
	job = e.actualPrevious(job)
	if job.Effect == "" {
		return "", preserveApplyFailure(job, fmt.Errorf("wallpaper: empty effect"))
	}
	if e.Capabilities().EngineFor(KindEffect) != EngineTerminal {
		return "", preserveApplyFailure(job, errors.New("wallpaper: sysc-terminal is not installed"))
	}
	socket := terminalSocketPath(e.dir, job.Connector)
	if e.ownedProcess(job.Connector) == nil || e.currentSocket(job.Connector) != socket {
		if err := clearDeadSocketIfPresent(job.Connector, socket); err != nil {
			return "", preserveApplyFailure(job, err)
		}
	}
	owned := e.ownedProcess(job.Connector)
	if err := e.stopOwned(job.Connector, e.currentSocket(job.Connector)); err != nil {
		if owned == nil {
			return "", preserveApplyFailure(job, err)
		}
		return "", err
	}
	if err := e.stopFallback(job.Connector); err != nil {
		return "", preserveApplyFailure(job, err)
	}
	e.clearActive(job.Connector)
	if err := e.launch(job.Connector, socket, terminalArgs(socket, job.Connector, job.Effect, job.Theme, job.Artwork)); err != nil {
		return "", e.restorePreviousAfterApplyFailure(job, set, err)
	}
	e.rememberApply(job, "", EngineTerminal, StatePlaying)
	return stillFor(job.Previous), nil
}

func preserveApplyFailure(job Job, err error) error {
	if !job.HasPrevious {
		return err
	}
	return &restoredApplyError{
		cause: err, state: job.PreviousState, engine: job.PreviousEngine,
		assignment: job.Previous, hasAssignment: true,
	}
}

// restorePreviousAfterApplyFailure is called under the connector lock after
// a replacement launch has stopped the prior wallpaper. Recreate the previous
// live assignment first, then fall back to its still if relaunching fails.
func (e *gslapperEngine) restorePreviousAfterApplyFailure(job Job, set Settings, applyErr error) error {
	if !job.HasPrevious {
		return applyErr
	}
	previous := job.Previous
	var restoreErr error
	switch {
	case previous.Kind == KindEffect && job.PreviousEngine == EngineTerminal &&
		(job.PreviousState == StatePlaying || job.PreviousState == StatePaused):
		socket := terminalSocketPath(e.dir, job.Connector)
		restoreErr = e.launch(job.Connector, socket, terminalArgs(socket, job.Connector, previous.Effect, previous.Theme, previous.Artwork))
		if restoreErr == nil {
			if job.PreviousState == StatePaused {
				if err := e.setPausedLocked(job.Connector, true); err != nil {
					previous.DesiredPlayback = StatePlaying
					e.setActive(job.Connector, previous, StatePlaying, EngineTerminal)
					return &restoredApplyError{
						cause: errors.Join(applyErr, fmt.Errorf("wallpaper: resume prior playback state: %w", err)),
						state: StatePlaying, engine: EngineTerminal, assignment: previous, hasAssignment: true,
					}
				}
			}
			e.setActive(job.Connector, previous, job.PreviousState, EngineTerminal)
			return &restoredApplyError{
				cause: applyErr, state: job.PreviousState, engine: EngineTerminal,
				assignment: previous, hasAssignment: true,
			}
		}
	case previous.Kind == KindImage && job.PreviousEngine == EngineGSlapper,
		previous.Kind == KindVideo && job.PreviousEngine == EngineGSlapper &&
			(job.PreviousState == StatePlaying || job.PreviousState == StatePaused):
		socket := socketPath(e.dir, job.Connector)
		restoreErr = e.launch(job.Connector, socket, launchArgs(set, socket, job.Connector, previous.Path))
		if restoreErr == nil {
			state := job.PreviousState
			if previous.Kind == KindImage {
				state = StateStatic
			}
			if state == StatePaused {
				if err := e.setPausedLocked(job.Connector, true); err != nil {
					previous.DesiredPlayback = StatePlaying
					e.setActive(job.Connector, previous, StatePlaying, EngineGSlapper)
					return &restoredApplyError{
						cause: errors.Join(applyErr, fmt.Errorf("wallpaper: resume prior playback state: %w", err)),
						state: StatePlaying, engine: EngineGSlapper, assignment: previous, hasAssignment: true,
					}
				}
			}
			e.setActive(job.Connector, previous, state, EngineGSlapper)
			return &restoredApplyError{
				cause: applyErr, state: state, engine: EngineGSlapper,
				assignment: previous, hasAssignment: true,
			}
		}
	}
	if still := stillFor(previous); still != "" {
		if err := e.startFallback(job.Connector, still); err == nil {
			engine := e.Capabilities().Static()
			e.setActive(job.Connector, previous, StateStatic, engine)
			return &restoredApplyError{
				cause: applyErr, state: StateStatic, engine: engine,
				assignment: previous, hasAssignment: true,
			}
		} else {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	if restoreErr == nil && job.PreviousState == StateStatic {
		e.setActive(job.Connector, previous, StateStatic, job.PreviousEngine)
		return &restoredApplyError{
			cause: applyErr, state: StateStatic, engine: job.PreviousEngine,
			assignment: previous, hasAssignment: true,
		}
	}
	if restoreErr == nil {
		restoreErr = errors.New("wallpaper: previous assignment has no available rollback")
	}
	e.clearActive(job.Connector)
	return errors.Join(applyErr, fmt.Errorf("wallpaper: restore previous assignment: %w", restoreErr))
}
