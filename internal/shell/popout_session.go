package shell

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func sessionTree(locker, errLabel string) *ui.Node {
	type action struct{ name, id string }
	acts := []action{
		{"Lock", "session-lock"},
		{"Log out", "session-logout"},
		{"Suspend", "session-suspend"},
		{"Reboot", "session-reboot"},
		{"Power off", "session-poweroff"},
	}
	if locker == "" {
		acts = acts[1:]
	}
	children := make([]*ui.Node, 0, len(acts)+1)
	if errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: errLabel, Tone: ui.ToneError})
	}
	for _, a := range acts {
		children = append(children, &ui.Node{
			Kind: ui.KindButton, Text: a.name, Action: a.id,
			Name: a.name, Role: "button", Focusable: true,
		})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: children}
}

func sessionArgv(action, locker string) []string {
	switch action {
	case "session-lock":
		return strings.Fields(locker)
	case "session-logout":
		return []string{"loginctl", "terminate-session", "self"}
	case "session-suspend":
		return []string{"loginctl", "suspend"}
	case "session-reboot":
		return []string{"loginctl", "reboot"}
	case "session-poweroff":
		return []string{"loginctl", "poweroff"}
	}
	return nil
}

// runArgv starts or runs one argv slice. Tests replace it.
var runArgv = runArgvDefault

func runArgvDefault(argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if argv[0] == "loginctl" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, path, argv[1:]...).Run()
	}
	return exec.Command(path, argv[1:]...).Start()
}
