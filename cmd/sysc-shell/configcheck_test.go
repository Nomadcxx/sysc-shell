package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConfigCheckUsageRefusals(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"too many paths", []string{"check", "a.json", "b.json"}, 2, "usage: sysc-shell config check [path]"},
		{"flag is refused", []string{"check", "--json"}, 2, "usage: sysc-shell config check [path]"},
		{"empty path", []string{"check", ""}, 2, "usage: sysc-shell config check [path]"},
		{"directory", []string{"check", "."}, 2, "config: read ."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := runConfigCheck(tc.args, &out, &errOut)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.wantCode, errOut.String())
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErr)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q, want empty on refusal", out.String())
			}
		})
	}
}

func TestRunConfigCheckReportsOKAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	var out, errOut bytes.Buffer
	if code := runConfigCheck([]string{"check"}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	want := filepath.Join(dir, "config", "sysc-shell", "config.json") + ": ok (defaults, file not found)\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}

	if err := os.WriteFile(path, []byte(`{"terminal-opacity":90}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := runConfigCheck([]string{"check", path}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if got, want := out.String(), path+": ok\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunConfigCheckReportsLoaderErrorsVerbatim(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"unknown field", `{"nope":1}`, 1, `config: `},
		{"opacity out of range", `{"terminal-opacity":10}`, 1, "config: terminal-opacity: 10 is outside 50..100"},
		{"blur out of range", `{"terminal-art":{"blur":900}}`, 1, "config: terminal-art.blur: must be in 0..64"},
		{"second value", `{"terminal-opacity":90} {}`, 1, "config: more than one JSON value"},
		{"second value with fields", `{"terminal-opacity":90} {"terminal-opacity":90}`, 1, "config: json: unknown field"},
		{"empty file", ``, 1, "config: EOF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := runConfigCheck([]string{"check", path}, &out, &errOut)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.wantCode, errOut.String())
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErr)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q, want empty on failure", out.String())
			}
		})
	}
}

func TestRunConfigCheckTouchesNoCompositorOrSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	runtime := filepath.Join(dir, "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("NIRI_SOCKET", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	var out, errOut bytes.Buffer
	if code := runConfigCheck([]string{"check"}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("runtime dir holds %v, want nothing: the check must not open a socket", entries)
	}
}

func TestRunConfigCheckOutputIsByteStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"terminal-opacity":10}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runConfigCheck([]string{"check", path}, &out, &errOut); code != 1 {
		t.Fatalf("code = %d", code)
	}
	want := "config: terminal-opacity: 10 is outside 50..100\n"
	if errOut.String() != want {
		t.Fatalf("stderr = %q, want exactly %q", errOut.String(), want)
	}
}

func TestRunConfigCheckRefusesAnIncompleteGrammar(t *testing.T) {
	// `sysc-shell config` on its own must not fall through into a compositor
	// run and must not quietly check a default path.
	cases := []struct {
		name string
		args []string
	}{
		{"no verb", nil},
		{"unknown verb", []string{"show"}},
		{"verb after a path", []string{"/tmp/config.json", "check"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := runConfigCheck(tc.args, &out, &errOut); code != 2 {
				t.Fatalf("code = %d, want 2 (stderr %q)", code, errOut.String())
			}
			if got, want := errOut.String(), "usage: sysc-shell config check [path]\n"; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", out.String())
			}
		})
	}
}

func TestRunConfigCheckRefusesWithoutAConfigHome(t *testing.T) {
	// With HOME and XDG_CONFIG_HOME unset there is no default path to resolve;
	// the refusal must say which input is missing.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	var out, errOut bytes.Buffer
	if code := runConfigCheck([]string{"check"}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2 (stderr %q)", code, errOut.String())
	}
	if got, want := errOut.String(), "config: no config path: HOME is unset\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}
