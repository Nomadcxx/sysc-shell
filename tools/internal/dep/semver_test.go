package dep

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		text    string
		wantErr bool
		major   int
		minor   int
		patch   int
		pre     string
		pseudo  bool
		wantOut string
	}{
		{text: "v1.2.3", major: 1, minor: 2, patch: 3, wantOut: "v1.2.3"},
		{text: "v0.0.0", major: 0, minor: 0, patch: 0, wantOut: "v0.0.0"},
		{text: "v1.2.3-rc.1", major: 1, minor: 2, patch: 3, pre: "rc.1", wantOut: "v1.2.3-rc.1"},
		{text: "v1.2.3+meta", major: 1, minor: 2, patch: 3, wantOut: "v1.2.3+meta"},
		{text: "v0.3.5-0.20260729084153-ddb7ff96ad4d", major: 0, minor: 3, patch: 5, pre: "0.20260729084153-ddb7ff96ad4d", pseudo: true, wantOut: "v0.3.5-0.20260729084153-ddb7ff96ad4d"},
		{text: "v0.0.0-20211118161319-6a13c67c3ce4", major: 0, minor: 0, patch: 0, pre: "20211118161319-6a13c67c3ce4", pseudo: true, wantOut: "v0.0.0-20211118161319-6a13c67c3ce4"},
		{text: "1.2.3", wantErr: true},
		{text: "v1.2", wantErr: true},
		{text: "v1.2.x", wantErr: true},
		{text: "", wantErr: true},
		{text: "v1.2.3-0.20260729084153-nothex", wantErr: false, major: 1, minor: 2, patch: 3, pre: "0.20260729084153-nothex", wantOut: "v1.2.3-0.20260729084153-nothex"},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.text)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) accepted a version it cannot read", c.text)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q) failed: %v", c.text, err)
			continue
		}
		if got.Major != c.major || got.Minor != c.minor || got.Patch != c.patch {
			t.Errorf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d", c.text, got.Major, got.Minor, got.Patch, c.major, c.minor, c.patch)
		}
		if got.Pre != c.pre {
			t.Errorf("ParseVersion(%q) pre = %q, want %q", c.text, got.Pre, c.pre)
		}
		if got.Pseudo != c.pseudo {
			t.Errorf("ParseVersion(%q) pseudo = %v, want %v", c.text, got.Pseudo, c.pseudo)
		}
		if got.String() != c.wantOut {
			t.Errorf("ParseVersion(%q).String() = %q, want %q", c.text, got.String(), c.wantOut)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{a: "v1.2.3", b: "v1.2.3", want: 0},
		{a: "v1.2.3", b: "v1.2.4", want: -1},
		{a: "v1.2.4", b: "v1.2.3", want: 1},
		{a: "v1.3.0", b: "v1.2.9", want: 1},
		{a: "v2.0.0", b: "v1.99.99", want: 1},
		{a: "v1.2.3-rc.1", b: "v1.2.3", want: -1},
		{a: "v1.2.3", b: "v1.2.3-rc.1", want: 1},
		{a: "v1.2.3-rc.1", b: "v1.2.3-rc.2", want: -1},
		{a: "v1.2.3-rc.10", b: "v1.2.3-rc.9", want: 1},
		{a: "v1.2.3-alpha", b: "v1.2.3-beta", want: -1},
		{a: "v1.2.3-alpha.1", b: "v1.2.3-alpha", want: 1},
		{a: "v1.2.3+meta", b: "v1.2.3", want: 0},
		{a: "v1.2.3-0.20260101000000-abcdefabcdef", b: "v1.2.3", want: -1},
		{a: "nonsense", b: "v1.2.3", want: -1},
		{a: "v1.2.3", b: "nonsense", want: 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSortVersions(t *testing.T) {
	versions := []string{"v0.2.0", "v0.1.0-rc.1", "v0.1.0", "v0.10.0", "v0.2.0-rc.1"}
	SortVersions(versions)
	want := []string{"v0.1.0-rc.1", "v0.1.0", "v0.2.0-rc.1", "v0.2.0", "v0.10.0"}
	for i := range want {
		if versions[i] != want[i] {
			t.Fatalf("SortVersions = %v, want %v", versions, want)
		}
	}
}

func TestEscapeModule(t *testing.T) {
	cases := []struct {
		module string
		want   string
	}{
		{module: "github.com/Nomadcxx/sysc-wayland", want: "github.com/!nomadcxx/sysc-wayland"},
		{module: "golang.org/x/net", want: "golang.org/x/net"},
		{module: "github.com/Nomadcxx/sysc-Go", want: "github.com/!nomadcxx/sysc-!go"},
	}
	for _, c := range cases {
		if got := EscapeModule(c.module); got != c.want {
			t.Errorf("EscapeModule(%q) = %q, want %q", c.module, got, c.want)
		}
	}
}

func TestReleasesBehind(t *testing.T) {
	published := []string{"v0.1.0", "v0.2.0", "v0.3.0", "v0.4.0-rc.1"}
	if got := ReleasesBehind("v0.1.0", published); got != 2 {
		t.Errorf("ReleasesBehind(v0.1.0) = %d, want 2", got)
	}
	if got := ReleasesBehind("v0.3.0", published); got != 0 {
		t.Errorf("ReleasesBehind(v0.3.0) = %d, want 0", got)
	}
}
