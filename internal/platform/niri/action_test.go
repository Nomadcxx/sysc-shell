package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestActionWritesFocusAndClose(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		body  any
		want  string
		reply string
		fail  bool
	}{
		{"focus", FocusWindow{ID: 80}, `{"Action":{"FocusWindow":{"id":80}}}`, `{"Ok":"Handled"}`, false},
		{"close", CloseWindow{ID: 80}, `{"Action":{"CloseWindow":{"id":80}}}`, `{"Ok":"Handled"}`, false},
		{"toggle overview", ToggleOverview{}, `{"Action":{"ToggleOverview":{}}}`, `{"Ok":"Handled"}`, false},
		{"close overview", CloseOverview{}, `{"Action":{"CloseOverview":{}}}`, `{"Ok":"Handled"}`, false},
		{
			"move window to workspace", MoveWindowToWorkspace{WindowID: 81, WorkspaceID: 7},
			`{"Action":{"MoveWindowToWorkspace":{"window_id":81,"reference":{"Id":7},"focus":false}}}`,
			`{"Ok":"Handled"}`, false,
		},
		{
			"move column to workspace", MoveColumnToWorkspace{WorkspaceID: 7},
			`{"Action":{"MoveColumnToWorkspace":{"reference":{"Id":7},"focus":false}}}`,
			`{"Ok":"Handled"}`, false,
		},
		{"err", FocusWindow{ID: 80}, `{"Action":{"FocusWindow":{"id":80}}}`, `{"Err":"no such window"}`, true},
		// The reference is an externally tagged enum in niri-ipc, so an id
		// reference is the object {"Id": n} and not a bare number.
		{
			"focus workspace", FocusWorkspace{ID: 7},
			`{"Action":{"FocusWorkspace":{"reference":{"Id":7}}}}`,
			`{"Ok":"Handled"}`, false,
		},

		{
			"screenshot screen", ScreenshotScreen{WriteToDisk: true, ShowPointer: true, Path: "/p/s.png"},
			`{"Action":{"ScreenshotScreen":{"write_to_disk":true,"show_pointer":true,"path":"/p/s.png"}}}`,
			`{"Ok":"Handled"}`, false,
		},
		// A null id is niri's "the focused window".
		{
			"screenshot focused window", ScreenshotWindow{WriteToDisk: true, Path: "/p/w.png"},
			`{"Action":{"ScreenshotWindow":{"id":null,"write_to_disk":true,"show_pointer":false,"path":"/p/w.png"}}}`,
			`{"Ok":"Handled"}`, false,
		},
		{
			"screenshot window by id", ScreenshotWindow{ID: new(uint64(9)), WriteToDisk: true, Path: "/p/w.png"},
			`{"Action":{"ScreenshotWindow":{"id":9,"write_to_disk":true,"show_pointer":false,"path":"/p/w.png"}}}`,
			`{"Ok":"Handled"}`, false,
		},
		{"power off", PowerOffMonitors{}, `{"Action":{"PowerOffMonitors":{}}}`, `{"Ok":"Handled"}`, false},
		{"power on", PowerOnMonitors{}, `{"Action":{"PowerOnMonitors":{}}}`, `{"Ok":"Handled"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			socket := filepath.Join(t.TempDir(), "niri.sock")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			got := make(chan string, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					got <- ""
					return
				}
				defer conn.Close()
				sc := bufio.NewScanner(conn)
				if !sc.Scan() {
					got <- ""
					return
				}
				got <- sc.Text()
				_, _ = conn.Write(append([]byte(tc.reply), '\n'))
			}()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = Action(ctx, socket, tc.body)
			if tc.fail {
				if err == nil {
					t.Fatal("Err reply returned nil")
				}
			} else if err != nil {
				t.Fatalf("Action: %v", err)
			}

			select {
			case line := <-got:
				var want, have any
				if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(line), &have); err != nil {
					t.Fatalf("request %q: %v", line, err)
				}
				wantJSON, _ := json.Marshal(want)
				haveJSON, _ := json.Marshal(have)
				if string(wantJSON) != string(haveJSON) {
					t.Fatalf("wrote %s, want %s", haveJSON, wantJSON)
				}
			case <-ctx.Done():
				t.Fatal("server got no request")
			}
		})
	}
}
