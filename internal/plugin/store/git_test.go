package store

import (
	"context"
	"path/filepath"
	"testing"
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
