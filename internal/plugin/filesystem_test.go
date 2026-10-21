package plugin

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func manifestWith(t *testing.T, minor int, filesystem any) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(timerManifest), &document); err != nil {
		t.Fatal(err)
	}
	document["protocol"].(map[string]any)["minor"] = minor
	if filesystem != nil {
		document["filesystem"] = filesystem
	}
	out, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFilesystemDeclarationIsValidatedAndVersioned(t *testing.T) {
	t.Parallel()
	fs := func(read, rw []string) map[string]any { return map[string]any{"read": read, "read_write": rw} }
	for _, tc := range []struct {
		name  string
		minor int
		decl  any
		err   string
	}{
		{"absent stays unconfined", 19, nil, ""},
		{"declared at 20", 20, fs([]string{"~/.local/share/opencode/auth.json"}, []string{"~/.cache/Proton"}), ""},
		{"an empty declaration asks for nothing", 20, map[string]any{}, ""},
		{"absolute paths", 20, fs([]string{"/srv/media"}, nil), ""},
		{"needs minor 20", 19, fs([]string{"~/.cache"}, nil), "requires protocol minor 20"},
		{"relative path", 20, fs([]string{".cache"}, nil), `filesystem.read[0]`},
		{"parent escape", 20, fs(nil, []string{"~/../etc"}), `filesystem.read_write[0]`},
		{"unclean path", 20, fs([]string{"~/.cache//x/"}, nil), `filesystem.read[0]`},
		{"the root", 20, fs([]string{"/"}, nil), `filesystem.read[0]`},
		{"unknown field", 20, map[string]any{"execute": []string{"~/bin"}}, "execute"},
	} {
		_, err := ParseManifest(manifestWith(t, tc.minor, tc.decl))
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: error %v, want one containing %q", tc.name, err, tc.err)
		}
	}
	m, err := ParseManifest(manifestWith(t, 20, fs([]string{"~/a"}, []string{"~/b"})))
	if err != nil {
		t.Fatal(err)
	}
	if m.Filesystem == nil || !slices.Equal(m.Filesystem.Read, []string{"~/a"}) || !slices.Equal(m.Filesystem.ReadWrite, []string{"~/b"}) {
		t.Fatalf("declaration = %+v", m.Filesystem)
	}
	if m, _ := ParseManifest(manifestWith(t, 19, nil)); m.Filesystem != nil {
		t.Fatal("an absent declaration decoded as one that asks for nothing")
	}
}

func TestPathRulesGrantOwnDirectoriesAndDeclarationsOnly(t *testing.T) {
	t.Parallel()
	m := Manifest{ID: "org.example.p", Dir: "/plugins/org.example.p",
		Filesystem: &Filesystem{Read: []string{"~/.local/share/opencode/auth.json"}, ReadWrite: []string{"~/.cache/Proton"}}}
	system := []PathRule{{Path: "/usr", Access: AccessReadExec}}
	got := pathRules(m, "/state", "/run/user/1/sysc-plugins/org.example.p", "/home/u", []string{"/run/user/1/bus"}, system)
	want := []PathRule{
		{Path: "/usr", Access: AccessReadExec},
		{Path: "/plugins/org.example.p", Access: AccessReadExec},
		{Path: "/state/org.example.p", Access: AccessReadWriteExec, Create: true},
		{Path: "/run/user/1/sysc-plugins/org.example.p", Access: AccessReadWrite, Create: true},
		{Path: "/run/user/1/bus", Access: AccessSocket},
		{Path: "/home/u/.local/share/opencode/auth.json", Access: AccessRead},
		{Path: "/home/u/.cache/Proton", Access: AccessReadWrite, Create: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rules =\n%+v\nwant\n%+v", got, want)
	}
	for _, r := range got {
		if r.Path == "/home/u" || strings.HasPrefix("/home/u/.ssh", r.Path+"/") {
			t.Fatalf("rule %+v reaches ~/.ssh", r)
		}
	}
}

func TestTheBoundaryFollowsTheDeclarationAndTheKernel(t *testing.T) {
	t.Parallel()
	declared := Manifest{Filesystem: &Filesystem{}}
	for _, tc := range []struct {
		name     string
		m        Manifest
		launcher string
		abi      int
		want     Boundary
		confines bool
	}{
		{"no launcher", declared, "", 5, BoundaryNone, false},
		{"no declaration", Manifest{}, "/bin/shell", 5, BoundaryLimited, false},
		{"declared, files only", declared, "/bin/shell", 1, BoundaryFilesUnscoped, true},
		{"declared, scopes but no socket right", declared, "/bin/shell", 6, BoundaryFilesOnly, true},
		{"declared, no socket right at 8", declared, "/bin/shell", 8, BoundaryFilesOnly, true},
		{"declared, sockets refused", declared, "/bin/shell", 9, BoundaryConfined, true},
		{"declared, no landlock", declared, "/bin/shell", 0, BoundaryNoLandlock, false},
	} {
		got, confines := boundaryOf(tc.m, tc.launcher, tc.abi)
		if got != tc.want || confines != tc.confines {
			t.Errorf("%s: boundary = %q confines=%v, want %q %v", tc.name, got, confines, tc.want, tc.confines)
		}
	}
}

func TestSocketsAndSignalsAreDeclaredWithTheFilesystem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		decl map[string]any
		err  string
	}{
		{"named endpoints", map[string]any{"sockets": []string{"session-bus", "system-bus", "compositor", "wayland"}}, ""},
		{"an absolute socket", map[string]any{"sockets": []string{"/run/docker.sock"}}, ""},
		{"unknown name", map[string]any{"sockets": []string{"x11"}}, `filesystem.sockets[0]`},
		{"home-relative socket", map[string]any{"sockets": []string{"~/.ssh/agent.sock"}}, `filesystem.sockets[0]`},
		{"unclean socket", map[string]any{"sockets": []string{"/run//docker.sock"}}, `filesystem.sockets[0]`},
		{"signals any", map[string]any{"signals": "any"}, ""},
		{"signals own", map[string]any{"signals": "own"}, ""},
		{"signals other", map[string]any{"signals": "all"}, `filesystem.signals`},
		{"sockets count toward the bound", map[string]any{"read": slices.Repeat([]string{"~/a"}, 30), "sockets": []string{"session-bus", "system-bus", "compositor"}}, "more than the 32 allowed"},
	} {
		_, err := ParseManifest(manifestWith(t, 20, tc.decl))
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: error %v, want one containing %q", tc.name, err, tc.err)
		}
	}
	m, err := ParseManifest(manifestWith(t, 20, map[string]any{"sockets": []string{"session-bus", "/run/docker.sock"}, "signals": "any"}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Filesystem.Sockets, []string{"session-bus", "/run/docker.sock"}) || !m.Filesystem.SignalAny {
		t.Fatalf("declaration = %+v", m.Filesystem)
	}
	if m, _ := ParseManifest(manifestWith(t, 20, map[string]any{})); m.Filesystem.SignalAny {
		t.Fatal("a declaration that names no signals may signal anything")
	}
}
