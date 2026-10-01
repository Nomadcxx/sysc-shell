package screenshot

import (
	"errors"
	"testing"
)

func TestDownloadsDir(t *testing.T) {
	reader := func(content string, err error) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(content), err }
	}
	cases := []struct {
		name string
		read func(string) ([]byte, error)
		want string
	}{
		{"home relative", reader(`XDG_DOWNLOAD_DIR="$HOME/Dl"`, nil), "/home/u/Dl"},
		{"absolute", reader(`XDG_DOWNLOAD_DIR="/data/dl"`, nil), "/data/dl"},
		{"other key only", reader(`XDG_PICTURES_DIR="$HOME/Pics"`, nil), "/home/u/Downloads"},
		{"disabled", reader(`XDG_DOWNLOAD_DIR="$HOME/"`, nil), "/home/u/Downloads"},
		{"relative value", reader(`XDG_DOWNLOAD_DIR="Dl"`, nil), "/home/u/Downloads"},
		{"unreadable", reader("", errors.New("no file")), "/home/u/Downloads"},
	}
	for _, c := range cases {
		if got := DownloadsDir("/home/u", "/home/u/.config", c.read); got != c.want {
			t.Errorf("%s: DownloadsDir = %q, want %q", c.name, got, c.want)
		}
	}
}
