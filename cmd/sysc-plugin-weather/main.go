package main

import (
	"context"
	"os"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/weather"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}

func run(in *os.File, out *os.File) error {
	c := v1.NewClient(in, out)
	if _, err := c.Handshake(v1.Identity{ID: "org.sysc.weather", Name: "Weather", Version: "1.0.0"}); err != nil {
		return err
	}

	var (
		svc     *weather.Service
		opt     = weather.ParseOptions(nil)
		last    weather.Snapshot
		updates <-chan weather.Snapshot
	)
	type view struct {
		kind v1.ViewKind
		rev  uint64
	}
	views := map[string]view{}
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
	ticks := time.NewTicker(time.Minute)
	defer ticks.Stop()

	ensure := func(values map[string]any) {
		cfg, err := weather.ParseConfig(values)
		if err != nil {
			return
		}
		opt = weather.ParseOptions(values)
		if svc == nil {
			svc = weather.New(cfg)
			updates = svc.Updates()
			last = svc.Snapshot()
			return
		}
		svc.Reconfigure(cfg)
	}

	publish := func(patch bool) {
		if svc != nil {
			last = svc.Snapshot()
		}
		for id, v := range views {
			if patch && v.rev > 0 {
				repl := weather.CurrentPatch(last, opt)
				if len(repl) == 0 {
					continue
				}
				next := v.rev + 1
				if err := c.Patch(id, v.rev, next, repl); err == nil {
					v.rev = next
					views[id] = v
					continue
				}
			}
			v.rev++
			views[id] = v
			var root *v1.Node
			switch v.kind {
			case v1.ViewBar:
				root = weather.BarTree(last, opt)
			case v1.ViewTooltip:
				root = weather.TooltipTree(last, opt)
			default:
				root = weather.PanelTree(last, opt)
			}
			_ = c.Snapshot(id, v.rev, root)
		}
	}

	for {
		select {
		case <-ctx.Done():
			if svc != nil {
				svc.Close()
			}
			return nil
		case snap := <-updates:
			last = snap
			publish(false)
		case <-ticks.C:
			if last.Stale() {
				publish(true)
			}
		case msg := <-incoming:
			switch m := msg.(type) {
			case *v1.HostShutdown:
				if svc != nil {
					svc.Close()
				}
				return nil
			case *v1.ViewOpen:
				if svc == nil {
					ensure(nil)
				}
				views[m.ViewID] = view{kind: m.View}
				publish(false)
			case *v1.ViewClose:
				delete(views, m.ViewID)
			case *v1.ViewResync:
				if v, ok := views[m.ViewID]; ok {
					v.rev = 0
					views[m.ViewID] = v
				}
				publish(false)
			case *v1.InputEvent:
				if m.Node == "open" {
					_, _ = c.Call(ctx, v1.CallPanelOpen, v1.PanelParams{Entry: "panel", Output: m.Output, Instance: m.ViewID})
				}
			case *v1.SettingsChanged:
				ensure(m.Values)
				publish(false)
			}
		}
	}
}
