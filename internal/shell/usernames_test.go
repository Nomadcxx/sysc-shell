package shell

import (
	"errors"
	"testing"
)

func TestUsernameCacheLooksUpOncePerUID(t *testing.T) {
	calls := map[string]int{}
	c := newUsernameCache(func(uid string) (string, error) {
		calls[uid]++
		if uid == "1000" {
			return "nomadx", nil
		}
		return "", errors.New("unknown user")
	})
	for i := 0; i < 3; i++ {
		if got := c.Name(1000); got != "nomadx" {
			t.Fatalf("Name(1000) = %q", got)
		}
		if got := c.Name(4242); got != "4242" {
			t.Fatalf("Name(4242) = %q, want the numeric id", got)
		}
	}
	if calls["1000"] != 1 || calls["4242"] != 1 {
		t.Fatalf("lookups = %v, want one per uid", calls)
	}
}
