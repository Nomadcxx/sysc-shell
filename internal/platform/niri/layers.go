package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// layersRequest is the one-shot layer-surface query.
const layersRequest = `"Layers"`

// Layer is one mapped layer-shell surface.
type Layer struct {
	Namespace string `json:"namespace"`
	Output    string `json:"output"`
	// Layer is Background, Bottom, Top, or Overlay.
	Layer string `json:"layer"`
}

type layersReply struct {
	Ok *struct {
		Layers []Layer `json:"Layers"`
	} `json:"Ok"`
	Err *string `json:"Err"`
}

// Layers asks the compositor which layer surfaces are mapped.
//
// This is a request and one reply on its own connection, unlike Stream, which
// holds the socket open for events. It exists so the wallpaper picker can say
// which output is already owned by somebody else's wallpaper instead of
// guessing from process names, which is the one thing the design forbids.
func Layers(ctx context.Context, socketPath string) ([]Layer, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("niri: connect to %s: %w", socketPath, err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	if _, err := fmt.Fprintln(conn, layersRequest); err != nil {
		return nil, fmt.Errorf("niri: send layers request: %w", err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), maxLine)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("niri: read layers: %w", err)
		}
		return nil, fmt.Errorf("niri: no layers reply")
	}
	return parseLayers(scanner.Bytes())
}

func parseLayers(line []byte) ([]Layer, error) {
	var reply layersReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, fmt.Errorf("niri: decode layers: %w", err)
	}
	if reply.Err != nil {
		return nil, fmt.Errorf("niri: layers: %s", *reply.Err)
	}
	if reply.Ok == nil {
		return nil, fmt.Errorf("niri: layers reply carried no result")
	}
	return reply.Ok.Layers, nil
}

// BackgroundOwners maps each output to the namespace of a Background-layer
// surface on it, ignoring any namespace in ours.
//
// A wallpaper is only visible if nothing else is painting the same layer over
// it, and gSlapper reports itself as playing either way, so this is the only
// honest signal that an apply will not be seen.
func BackgroundOwners(layers []Layer, ours func(namespace string) bool) map[string]string {
	out := map[string]string{}
	for _, l := range layers {
		if l.Layer != "Background" {
			continue
		}
		if ours != nil && ours(l.Namespace) {
			continue
		}
		if _, seen := out[l.Output]; !seen {
			out[l.Output] = l.Namespace
		}
	}
	return out
}
