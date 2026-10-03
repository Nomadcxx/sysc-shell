package wallpaper

import (
	"bytes"
	"errors"
	"fmt"
)

func listSupports(out []byte) bool {
	return bytes.Contains(out, []byte("effect "))
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
