package dep

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// api is a stand-in for the GitHub API. It answers the handful of calls the
// tools make, so the client can be checked without a network.
func api(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.RequestURI()]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// content encodes a file the way the contents API does.
func content(text string) string {
	return `{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(text)) + `"}`
}

func TestClientRepos(t *testing.T) {
	server := api(t, map[string]string{
		"/user/repos?per_page=100&affiliation=owner": `[{"name":"sysc-shell","default_branch":"main","private":false,"archived":false}]`,
	})
	client := NewClient("token")
	client.BaseURL = server.URL
	repos, err := client.Repos(context.Background(), "Nomadcxx")
	if err != nil {
		t.Fatalf("Repos failed: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "sysc-shell" || repos[0].DefaultBranch != "main" {
		t.Fatalf("Repos = %+v", repos)
	}
}

func TestClientFile(t *testing.T) {
	server := api(t, map[string]string{
		"/repos/Nomadcxx/sysc-shell/contents/go.mod?ref=main": content("module example.com/x\n"),
	})
	client := NewClient("")
	client.BaseURL = server.URL
	data, err := client.File(context.Background(), "Nomadcxx", "sysc-shell", "go.mod", "main")
	if err != nil {
		t.Fatalf("File failed: %v", err)
	}
	if string(data) != "module example.com/x\n" {
		t.Fatalf("File read %q", data)
	}
	if _, err := client.File(context.Background(), "Nomadcxx", "sysc-shell", "missing", "main"); err == nil {
		t.Fatalf("File accepted a path the API does not have")
	}
}

func TestClientListDir(t *testing.T) {
	server := api(t, map[string]string{
		"/repos/Nomadcxx/sysc-shell/contents/.github/workflows?ref=main": `[{"name":"ci.yml","path":".github/workflows/ci.yml","type":"file"},{"name":"sub","path":".github/workflows/sub","type":"dir"}]`,
	})
	client := NewClient("")
	client.BaseURL = server.URL
	entries, err := client.ListDir(context.Background(), "Nomadcxx", "sysc-shell", ".github/workflows", "main")
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "ci.yml" || entries[1].Type != "dir" {
		t.Fatalf("ListDir = %+v", entries)
	}
}

func TestClientHeadAndCompare(t *testing.T) {
	server := api(t, map[string]string{
		"/repos/Nomadcxx/sysc-plugins/commits/main":                `{"sha":"04fa71c9"}`,
		"/repos/Nomadcxx/sysc-plugins/compare/0cea6f74...04fa71c9": `{"ahead_by":46,"behind_by":0,"status":"behind"}`,
	})
	client := NewClient("")
	client.BaseURL = server.URL
	sha, err := client.HeadSHA(context.Background(), "Nomadcxx", "sysc-plugins", "main")
	if err != nil {
		t.Fatalf("HeadSHA failed: %v", err)
	}
	if sha != "04fa71c9" {
		t.Fatalf("HeadSHA = %q", sha)
	}
	ahead, err := client.CommitsAhead(context.Background(), "Nomadcxx", "sysc-plugins", "0cea6f74", sha)
	if err != nil {
		t.Fatalf("CommitsAhead failed: %v", err)
	}
	if ahead != 46 {
		t.Fatalf("CommitsAhead = %d, want 46", ahead)
	}
}

func TestClientReportsAMissingPath(t *testing.T) {
	server := api(t, map[string]string{})
	client := NewClient("")
	client.BaseURL = server.URL
	_, err := client.File(context.Background(), "Nomadcxx", "sysc-shell", "go.mod", "main")
	if err == nil {
		t.Fatalf("File accepted a path the API does not have")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("the error does not say the path is missing: %v", err)
	}
}

func TestVersions(t *testing.T) {
	server := api(t, map[string]string{
		"/github.com/!nomadcxx/sysc-wayland/@v/list": "v0.3.2\nv0.3.0\nv0.2.0\n",
	})
	versions, err := Versions(context.Background(), server.Client(), server.URL, "github.com/Nomadcxx/sysc-wayland")
	if err != nil {
		t.Fatalf("Versions failed: %v", err)
	}
	if len(versions) != 3 || versions[0] != "v0.2.0" || versions[2] != "v0.3.2" {
		t.Fatalf("Versions = %v", versions)
	}
	latest, err := LatestVersion(context.Background(), server.Client(), server.URL, "github.com/Nomadcxx/sysc-wayland")
	if err != nil {
		t.Fatalf("LatestVersion failed: %v", err)
	}
	if latest != "v0.3.2" {
		t.Fatalf("LatestVersion = %q", latest)
	}
}

func TestVersionsOfAModuleTheProxyDoesNotCarry(t *testing.T) {
	server := api(t, map[string]string{})
	_, err := Versions(context.Background(), server.Client(), server.URL, "example.com/missing")
	if err == nil {
		t.Fatalf("Versions accepted a module the proxy does not carry")
	}
	if !strings.Contains(err.Error(), ErrNotPublished.Error()) {
		t.Fatalf("the error does not say the module is not published: %v", err)
	}
}
