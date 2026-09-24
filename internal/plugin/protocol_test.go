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
