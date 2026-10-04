package plugin

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestHostSupports(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    v1.Version
		want bool
	}{
		{v1.Version{Major: 1, Minor: 0}, true},
		{v1.Version{Major: 1, Minor: HostProtocolMinor}, true},
		{v1.Version{Major: 1, Minor: HostProtocolMinor + 1}, false},
		{v1.Version{Major: 2, Minor: 0}, false},
		{v1.Version{Major: 0, Minor: 9}, false},
		{v1.Version{Major: 1, Minor: -1}, false},
	}
	for _, c := range cases {
		if got := HostSupports(c.v); got != c.want {
			t.Errorf("HostSupports(%+v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestHostSupportedVersionsNamesEveryMinor(t *testing.T) {
	t.Parallel()
	got := hostSupportedVersions()
	if len(got) != HostProtocolMinor+1 {
		t.Fatalf("len = %d, want %d", len(got), HostProtocolMinor+1)
	}
	if got[0] != (v1.Version{Major: 1, Minor: HostProtocolMinor}) {
		t.Fatalf("best = %+v, want 1.%d", got[0], HostProtocolMinor)
	}
	if got[len(got)-1] != (v1.Version{Major: 1, Minor: 0}) {
		t.Fatalf("oldest = %+v, want 1.0", got[len(got)-1])
	}
	seen15 := false
	for i, v := range got {
		if v.Major != 1 || v.Minor != HostProtocolMinor-i {
			t.Fatalf("got[%d] = %+v, want 1.%d", i, v, HostProtocolMinor-i)
		}
		if v.Minor == 15 {
			seen15 = true
		}
	}
	if HostProtocolMinor >= 15 && !seen15 {
		t.Fatal("minor 15 missing from host.hello Supported")
	}
}
