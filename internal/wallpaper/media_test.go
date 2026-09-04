package wallpaper

import "testing"

func TestClassifyName(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
	}{
		{"a.jpg", KindImage},
		{"a.JPEG", KindImage},
		{"a.png", KindImage},
		{"a.webp", KindImage},
		{"a.jxl", KindImage},
		{"a.bmp", KindImage},
		{"a.gif", KindVideo},
		{"a.mp4", KindVideo},
		{"a.mkv", KindVideo},
		{"a.webm", KindVideo},
		{"a.mov", KindVideo},
		{"a.avi", KindVideo},
		{"a.m4v", KindVideo},
		{"a.txt", KindUnknown},
		{"noext", KindUnknown},
	}
	for _, c := range cases {
		if got := ClassifyName(c.name); got != c.kind {
			t.Errorf("ClassifyName(%q) = %v, want %v", c.name, got, c.kind)
		}
	}
}

func TestSocketPath(t *testing.T) {
	got := socketPath("/run/user/1000/sysc-shell", "DP-1")
	if got != "/run/user/1000/sysc-shell/gslapper-DP-1.sock" {
		t.Fatalf("got %q", got)
	}
	if socketPath("/run/user/1000/sysc-shell", "HDMI-A-1") == got {
		t.Fatal("connectors must not share a socket")
	}
	if socketPath("/tmp", "DP-1; rm -rf /") != "/tmp/gslapper-DP-1-rm--rf-.sock" {
		t.Fatalf("sanitize connector to [A-Za-z0-9._-], got %q", socketPath("/tmp", "DP-1; rm -rf /"))
	}
}
