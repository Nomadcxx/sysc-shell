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
		{"err", FocusWindow{ID: 80}, `{"Action":{"FocusWindow":{"id":80}}}`, `{"Err":"no such window"}`, true},
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
				var want, have json.RawMessage
				if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(line), &have); err != nil {
					t.Fatalf("request %q: %v", line, err)
				}
				if string(want) != string(have) {
					t.Fatalf("wrote %s, want %s", have, want)
				}
			case <-ctx.Done():
				t.Fatal("server got no request")
			}
		})
	}
}
