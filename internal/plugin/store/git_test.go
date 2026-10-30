package store

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitCatalogClonesThenFetches(t *testing.T) {
	t.Parallel()
	repo := newGitRepo(t)
	first := repo.publish([]byte(`{"schema":1,"plugins":[]}`))
	cache := filepath.Join(t.TempDir(), "sources", "test")
	ctx := context.Background()

	body, commit, err := Git{}.Catalog(ctx, cache, repo.url())
	if err != nil || string(body) != `{"schema":1,"plugins":[]}` || commit != first {
		t.Fatalf("first read = %q, %s, %v; want commit %s", body, commit, err, first)
	}

	second := repo.publish([]byte(`{"schema":1,"plugins":[{}]}`))
	body, commit, err = Git{}.Catalog(ctx, cache, repo.url())
	if err != nil || string(body) != `{"schema":1,"plugins":[{}]}` || commit != second {
		t.Fatalf("second read = %q, %s, %v; want commit %s", body, commit, err, second)
	}
}

func TestGitCatalogReclonesWhenTheSourceMoves(t *testing.T) {
	t.Parallel()
	a, b := newGitRepo(t), newGitRepo(t)
	a.publish([]byte(`"a"`))
	want := b.publish([]byte(`"b"`))
	cache := filepath.Join(t.TempDir(), "test")
	if _, _, err := (Git{}).Catalog(context.Background(), cache, a.url()); err != nil {
		t.Fatal(err)
	}
	body, commit, err := Git{}.Catalog(context.Background(), cache, b.url())
	if err != nil || string(body) != `"b"` || commit != want {
		t.Fatalf("read = %q, %s, %v", body, commit, err)
	}
}

func TestGitCatalogErrors(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	if _, _, err := (Git{Bin: "sysc-no-such-git"}).Catalog(ctx, t.TempDir(), "file:///x"); KindOf(err) != KindGitMissing {
		t.Errorf("missing git: %v", err)
	}
	if _, _, err := (Git{}).Catalog(ctx, filepath.Join(t.TempDir(), "c"), "file:///sysc/no/such/repo"); KindOf(err) != KindUnreachable {
		t.Errorf("missing repo: %v", err)
	}
	empty := newGitRepo(t)
	empty.git("commit", "-q", "--allow-empty", "-m", "nothing")
	if _, _, err := (Git{}).Catalog(ctx, filepath.Join(t.TempDir(), "c"), empty.url()); KindOf(err) != KindUnreachable {
		t.Errorf("repo without catalog.json: %v", err)
	}
}

func TestGitCatalogBoundsStalledHTTPRemote(t *testing.T) {
	requireGit(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	requestClosed := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		_, _ = io.Copy(io.Discard, conn)
		close(requestClosed)
	}()

	const timeout = 250 * time.Millisecond
	done := make(chan error, 1)
	returned := false
	var serverConn net.Conn
	cache := filepath.Join(t.TempDir(), "source")
	defer func() {
		_ = listener.Close()
		if serverConn != nil {
			_ = serverConn.Close()
		}
		<-serverDone
		if !returned {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("Catalog goroutine did not exit after closing the stalled remote")
			}
		}
	}()
	go func() {
		_, _, err := (Git{Timeout: timeout}).Catalog(context.Background(), cache, "http://"+listener.Addr().String()+"/stalled.git")
		done <- err
	}()

	select {
	case serverConn = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("git never contacted the stalled remote")
	}

	select {
	case err := <-done:
		returned = true
		if KindOf(err) != KindGitTimeout {
			t.Fatalf("Catalog error = %v, want %s", err, KindGitTimeout)
		}
		if !strings.Contains(err.Error(), "git clone") {
			t.Fatalf("timeout error = %v, want operation name", err)
		}
	case <-time.After(timeout + 2*time.Second):
		t.Fatal("Catalog stayed blocked after its git timeout")
	}

	select {
	case <-requestClosed:
	case <-time.After(time.Second):
		t.Fatal("remote helper kept the stalled HTTP request open after timeout")
	}
}
