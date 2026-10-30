package main

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/worldclock"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}

func run(in *os.File, out *os.File) error {
	c := v1.NewClient(in, out)
	if _, err := c.Handshake(v1.Identity{ID: "org.sysc.world-clock", Name: "World Clock", Version: "1.0.0"}); err != nil {
		return err
	}
	clk := worldclock.New()
	type view struct {
		kind     v1.ViewKind
		rev      uint64
		instance string
	}
	views := map[string]view{}
	draft := ""
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	incoming := make(chan v1.Message, 8)
	go func() {
		for {
			msg, err := c.Recv()
			if err != nil {
				cancel()
				return
			}
			incoming <- msg
		}
	}()
	restore(ctx, c, clk)
	ticks := time.NewTicker(time.Second)
	defer ticks.Stop()

	snapshot := func() {
		readings := clk.Readings(time.Now())
		first := worldclock.Reading{}
		if len(readings) > 0 {
			first = readings[0]
		}
		for id, v := range views {
			v.rev++
			views[id] = v
			switch v.kind {
			case v1.ViewBar:
				_ = c.Snapshot(id, v.rev, worldclock.BarTree(first))
			default:
				_ = c.Snapshot(id, v.rev, worldclock.PanelTree(readings, clk.PendingAdd(), clk.PendingRemove(), draft))
			}
		}
	}
	patchTimes := func() {
		readings := clk.Readings(time.Now())
		var first worldclock.Reading
		if len(readings) > 0 {
			first = readings[0]
		}
		panelRepl := worldclock.TimePatch(readings)
		barRepl := worldclock.BarTimePatch(first)
		for id, v := range views {
			if v.rev == 0 {
				continue
			}
			repl := panelRepl
			if v.kind == v1.ViewBar {
				repl = barRepl
			}
			next := v.rev + 1
			if err := c.Patch(id, v.rev, next, repl); err != nil {
				continue
			}
			v.rev = next
			views[id] = v
		}
	}

	snapshot()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticks.C:
			patchTimes()
		case msg := <-incoming:
			switch m := msg.(type) {
			case *v1.HostShutdown:
				return nil
			case *v1.ViewOpen:
				views[m.ViewID] = view{kind: m.View, instance: m.Instance}
				snapshot()
			case *v1.ViewClose:
				delete(views, m.ViewID)
			case *v1.ViewResync:
				if v, ok := views[m.ViewID]; ok {
					v.rev = 0
					views[m.ViewID] = v
				}
				snapshot()
			case *v1.InputEvent:
				if v, ok := views[m.ViewID]; ok {
					handle(ctx, c, clk, &draft, m, v.instance)
					snapshot()
				}
			case *v1.SettingsChanged:
				if raw, ok := m.Values["hour24"]; ok {
					switch v := raw.(type) {
					case bool:
						clk.SetHour24(v)
					case float64:
						clk.SetHour24(v != 0)
					}
					snapshot()
				}
			}
		}
	}
}

func handle(ctx context.Context, c *v1.Client, clk *worldclock.Clock, draft *string, m *v1.InputEvent, instance string) {
	switch {
	case m.Node == "open":
		_, _ = c.Call(ctx, v1.CallPanelOpen, panelOpenParams(instance, m))
	case m.Node == "zone":
		*draft = m.Text
		if m.Event == v1.EventSubmit {
			_ = clk.ProposeAdd(strings.TrimSpace(*draft))
		}
	case m.Node == "confirm-add":
		clk.ConfirmAdd()
		*draft = ""
		save(ctx, c, clk)
	case m.Node == "confirm-remove":
		clk.ConfirmRemove()
		save(ctx, c, clk)
	case m.Node == "cancel":
		clk.CancelPending()
	case strings.HasPrefix(m.Node, "rm:"):
		clk.ProposeRemove(strings.TrimPrefix(m.Node, "rm:"))
	case strings.HasPrefix(m.Node, "drop:") && m.Event == v1.EventDrop:
		zones := clk.Zones()
		insert, ok := dropInsertBefore(m.Node, len(zones))
		if !ok {
			return
		}
		from := -1
		for i, z := range zones {
			if z == m.Text {
				from = i
				break
			}
		}
		if from >= 0 {
			_ = clk.Reorder(from, insert)
			save(ctx, c, clk)
		}
	}
}

func panelOpenParams(instance string, m *v1.InputEvent) v1.PanelParams {
	return v1.PanelParams{Entry: "panel", Output: m.Output, Generation: m.Generation, Instance: instance}
}

func dropInsertBefore(node string, zoneCount int) (int, bool) {
	if !strings.HasPrefix(node, "drop:") {
		return 0, false
	}
	gap, err := strconv.Atoi(strings.TrimPrefix(node, "drop:"))
	if err != nil || gap < 0 || gap > zoneCount {
		return 0, false
	}
	return gap, true
}

func save(ctx context.Context, c *v1.Client, clk *worldclock.Clock) {
	raw, _ := json.Marshal(clk.Zones())
	_, _ = c.Call(ctx, v1.CallStateSet, v1.StateSetParams{Key: "zones", Value: raw})
}

func restore(ctx context.Context, c *v1.Client, clk *worldclock.Clock) {
	reply, err := c.Call(ctx, v1.CallStateGet, v1.StateGetParams{Key: "zones"})
	if err != nil || !reply.OK {
		return
	}
	var result v1.StateGetResult
	if err := json.Unmarshal(reply.Result, &result); err != nil || !result.Found {
		return
	}
	var zones []string
	if err := json.Unmarshal(result.Value, &zones); err != nil {
		return
	}
	clk.Restore(zones)
}
