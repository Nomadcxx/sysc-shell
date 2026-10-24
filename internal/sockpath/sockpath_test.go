package sockpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirLeavesRoomForASocket(t *testing.T) {
	for _, prefix := range []string{"niri", "wayland", "syt"} {
		t.Run(prefix, func(t *testing.T) {
			dir := Dir(t, prefix)
			if dir == "" {
				t.Fatal("Dir returned an empty path")
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("the directory does not exist: %v", err)
			}
			path := filepath.Join(dir, "niri.sock")
			if len(path) > Limit {
				t.Fatalf("the socket path is %d bytes: %s; the limit is %d", len(path), path, Limit)
			}
		})
	}
}

func TestDirStaysShortUnderALongTempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), strings.Repeat("long", 20)))
	dir := Dir(t, "niri")
	if len(dir)+Reserve > Limit {
		t.Fatalf("the directory is %d bytes: %s; the limit leaves room for %d bytes", len(dir), dir, Reserve)
	}
}

func TestDirRemovesTheDirectoryWhenTheTestEnds(t *testing.T) {
	var dir string
	t.Run("inner", func(t *testing.T) {
		dir = Dir(t, "niri")
	})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory outlived the test: %v", err)
	}
}

func TestMakeDirRejectsABaseThatIsTooLong(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 80))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	saved := bases
	bases = nil
	t.Cleanup(func() { bases = saved })
	t.Setenv("TMPDIR", long)
	if _, err := makeDir("niri"); err == nil {
		t.Fatal("makeDir accepted a base whose path overruns the socket limit")
	}
}

func TestCheckSocketPaths(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "a socket under t.TempDir",
			body: `listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "niri.sock"))`,
			want: true,
		},
		{
			name: "a socket under a temp dir variable",
			body: `dir := t.TempDir()
				socket := filepath.Join(dir, "niri.sock")
				listener, err := net.Listen("unix", socket)`,
			want: true,
		},
		{
			name: "a socket under a struct field",
			body: `f := &fake{socket: filepath.Join(t.TempDir(), "niri.sock")}
				listener, err := net.Listen("unix", f.socket)`,
			want: true,
		},
		{
			name: "a unix address under t.TempDir",
			body: `listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "niri.sock"), Net: "unix"})`,
			want: true,
		},
		{
			name: "a socket under os.MkdirTemp",
			body: `dir, err := os.MkdirTemp("", "niri")
				listener, err := net.Listen("unix", filepath.Join(dir, "niri.sock"))`,
			want: true,
		},
		{
			name: "a socket under a short dir",
			body: `listener, err := net.Listen("unix", filepath.Join(sockpath.Dir(t, "niri"), "niri.sock"))`,
			want: false,
		},
		{
			name: "a bus address under t.TempDir",
			body: `dir := t.TempDir()
				addr := "unix:path=" + filepath.Join(dir, "bus")`,
			want: true,
		},
		{
			name: "a bus address under a short dir",
			body: `addr := "unix:path=" + filepath.Join(sockpath.Dir(t, "dbus"), "bus")`,
			want: false,
		},
		{
			name: "no socket at all",
			body: `dir := t.TempDir()
				_ = filepath.Join(dir, "config.json")`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, "fixture_test.go", tc.body)
			err := CheckSocketPaths(dir)
			if tc.want && err == nil {
				t.Fatal("the check accepted a socket under a long temporary directory")
			}
			if !tc.want && err != nil {
				t.Fatalf("the check rejected a sound socket path: %v", err)
			}
		})
	}
}

// TestCountSocketBindsCountsWhatTheCheckSees pins the count a package uses
// to tell a passing check from a check that stopped reading its tests.
func TestCountSocketBindsCountsWhatTheCheckSees(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fixture_test.go", `listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "niri.sock"))`)
	count, err := CountSocketBinds(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the count is %d, want 1", count)
	}
}

// writeFixture writes a test file whose body holds the code under check.
func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	source := "package fixture\n\nimport (\n\t\"net\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n\n\t\"github.com/Nomadcxx/sysc-shell/internal/sockpath\"\n)\n\nfunc fixture(t *testing.T) {\n\t" + body + "\n\t_, _ = listener, err\n}\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
