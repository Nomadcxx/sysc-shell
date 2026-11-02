package shell

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func TestSpawnDesktopExecUsesIPC(t *testing.T) {
	var sent []any
	r := &Registry{
		niriSend: func(body any) error { sent = append(sent, body); return nil },
		runArgv:  func(argv []string) error { t.Fatalf("external launch: %v", argv); return nil },
	}
	r.spawnDesktopExecLocked(`foot -e "hello world" %U`)
	want := []any{niri.Spawn{Command: []string{"foot", "-e", "hello world"}}}
	if !reflect.DeepEqual(sent, want) {
		t.Fatalf("actions = %#v, want %#v", sent, want)
	}
}
