package main

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/timer"
)

func TestBarReadoutOpensTimerPanelOnItsOutput(t *testing.T) {
	pluginIn, hostIn := io.Pipe()
	hostOut, pluginOut := io.Pipe()
	c := v1.NewClient(pluginIn, pluginOut)
	decoder := v1.NewDecoder(hostOut, v1.ToHost)
	encoder := v1.NewEncoder(hostIn)
	got := make(chan *v1.HostCall, 1)
	hostErr := make(chan error, 1)
	go func() {
		msg, err := decoder.Decode()
		if err != nil {
			hostErr <- err
			return
		}
		call, ok := msg.(*v1.HostCall)
		if !ok {
			hostErr <- io.ErrUnexpectedEOF
			return
		}
		got <- call
		hostErr <- encoder.Encode(&v1.HostReply{ID: call.ID, OK: true})
	}()
	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		_, _ = c.Recv()
	}()
	defer func() {
		_ = hostIn.Close()
		_ = pluginOut.Close()
		<-recvDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	handleInput(ctx, c, timer.New(time.Now), &v1.InputEvent{
		ViewID: "timer-bar", Node: "open", Event: v1.EventActivate, Output: "DP-1",
	})
	if err := ctx.Err(); err != nil {
		t.Fatalf("opening the panel timed out: %v", err)
	}
	call := <-got
	if err := <-hostErr; err != nil {
		t.Fatal(err)
	}
	if call.Call != v1.CallPanelOpen {
		t.Fatalf("host call = %q, want %q", call.Call, v1.CallPanelOpen)
	}
	var params v1.PanelParams
	if err := json.Unmarshal(call.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Entry != "panel" || params.Output != "DP-1" || params.Instance != "timer-bar" {
		t.Fatalf("panel params = %+v, want panel on DP-1 for timer-bar", params)
	}
}
