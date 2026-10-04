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

func (e *gslapperEngine) applyEffect(job Job) (string, error) {
	if job.Effect == "" {
		return "", fmt.Errorf("wallpaper: empty effect")
	}
	unlock := e.lockConnector(job.Connector)
	defer unlock()
	if e.isClosed() {
		return "", errEngineClosed
	}
	if e.Capabilities().EngineFor(KindEffect) != EngineTerminal {
		return "", errors.New("wallpaper: sysc-terminal is not installed")
	}
	if err := e.stopFallback(job.Connector); err != nil {
		return "", err
	}
	if err := e.stopOwned(job.Connector, e.currentSocket(job.Connector)); err != nil {
		return "", err
	}
	socket := terminalSocketPath(e.dir, job.Connector)
	if err := e.launch(job.Connector, socket, terminalArgs(socket, job.Connector, job.Effect, job.Theme, job.Artwork)); err != nil {
		return "", err
	}
	return "", nil
}
