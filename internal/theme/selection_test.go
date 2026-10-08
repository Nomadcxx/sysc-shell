package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell-theme")
	for _, name := range []string{"dracula", "blue", ""} {
		if err := PublishSelection(path, name); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != name+"\n" {
			t.Fatalf("got %q %v", got, err)
		}
	}
	if err := PublishSelection(path, "../bad"); err == nil {
		t.Fatal("accepted unknown theme")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "\n" {
		t.Fatal("invalid selection changed file")
	}
}
