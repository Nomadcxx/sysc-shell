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
		{"absent stays unconfined", 17, nil, ""},
		{"declared at 18", 18, fs([]string{"~/.local/share/opencode/auth.json"}, []string{"~/.cache/Proton"}), ""},
		{"an empty declaration asks for nothing", 18, map[string]any{}, ""},
		{"absolute paths", 18, fs([]string{"/srv/media"}, nil), ""},
		{"needs minor 18", 17, fs([]string{"~/.cache"}, nil), "requires protocol minor 18"},
		{"relative path", 18, fs([]string{".cache"}, nil), `filesystem.read[0]`},
		{"parent escape", 18, fs(nil, []string{"~/../etc"}), `filesystem.read_write[0]`},
		{"unclean path", 18, fs([]string{"~/.cache//x/"}, nil), `filesystem.read[0]`},
		{"the root", 18, fs([]string{"/"}, nil), `filesystem.read[0]`},
		{"unknown field", 18, map[string]any{"execute": []string{"~/bin"}}, "execute"},
	} {
		_, err := ParseManifest(manifestWith(t, tc.minor, tc.decl))
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: error %v, want one containing %q", tc.name, err, tc.err)
		}
	}
	m, err := ParseManifest(manifestWith(t, 18, fs([]string{"~/a"}, []string{"~/b"})))
	if err != nil {
		t.Fatal(err)
	}
	if m.Filesystem == nil || !slices.Equal(m.Filesystem.Read, []string{"~/a"}) || !slices.Equal(m.Filesystem.ReadWrite, []string{"~/b"}) {
		t.Fatalf("declaration = %+v", m.Filesystem)
	}
	if m, _ := ParseManifest(manifestWith(t, 17, nil)); m.Filesystem != nil {
		t.Fatal("an absent declaration decoded as one that asks for nothing")
	}
}

func TestPathRulesGrantOwnDirectoriesAndDeclarationsOnly(t *testing.T) {
	t.Parallel()
	m := Manifest{ID: "org.example.p", Dir: "/plugins/org.example.p",
		Filesystem: &Filesystem{Read: []string{"~/.local/share/opencode/auth.json"}, ReadWrite: []string{"~/.cache/Proton"}}}
	system := []PathRule{{Path: "/usr", Access: AccessReadExec}}
	got := pathRules(m, "/state", "/home/u", system)
	want := []PathRule{
		{Path: "/usr", Access: AccessReadExec},
		{Path: "/plugins/org.example.p", Access: AccessReadExec},
		{Path: "/state/org.example.p", Access: AccessReadWriteExec, Create: true},
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
		{"declared, landlock", declared, "/bin/shell", 1, BoundaryConfined, true},
		{"declared, no landlock", declared, "/bin/shell", 0, BoundaryNoLandlock, false},
	} {
		got, confines := boundaryOf(tc.m, tc.launcher, tc.abi)
		if got != tc.want || confines != tc.confines {
			t.Errorf("%s: boundary = %q confines=%v, want %q %v", tc.name, got, confines, tc.want, tc.confines)
		}
	}
}
