package ipc

import (
	"encoding/json"
	"testing"
)

func TestControlCentreSectionIsForwarded(t *testing.T) {
	var got [3]string
	srv := NewServer("", Handlers{Panel: func(action, panel, section string) error {
		got = [3]string{action, panel, section}
		return nil
	}})
	out := srv.handleLine(`{"id":1,"method":"panel.open","params":{"panel":"control-center","section":"audio"}}`)
	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" {
		t.Fatalf("panel.open failed: %s", response.Error)
	}
	if got != [3]string{"open", "control-center", "audio"} {
		t.Errorf("handler got %q, want action, panel and section", got)
	}
}
