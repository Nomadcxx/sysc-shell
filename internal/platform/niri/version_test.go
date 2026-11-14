package niri

import (
	"context"
	"testing"
	"time"
)

func TestVersionQuery(t *testing.T) {
	f := startFakeNiri(t, `{"Ok":{"Version":"26.04"}}`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := Version(ctx, f.path)
	if err != nil || got != "26.04" {
		t.Fatalf("Version = %q, %v", got, err)
	}
	if req := <-f.requests; req != "\"Version\"\n" {
		t.Fatalf("request=%q", req)
	}
}
