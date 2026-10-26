package dep

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// APIBase is the GitHub API the client reads.
const APIBase = "https://api.github.com"

// Repo is a repository the client read.
type Repo struct {
	// Name is the repository name.
	Name string
	// DefaultBranch is the branch the API reports as the default.
	DefaultBranch string
	// Private is true when the repository is not public.
	Private bool
	// Archived is true when the repository is archived.
	Archived bool
}

// Entry is one item of a repository directory.
type Entry struct {
	// Name is the item name.
	Name string
	// Path is the item path.
	Path string
	// Type is the item type: "file" or "dir".
	Type string
}

// Client reads the GitHub API.
//
// It reads only. The reporter is a report, and the bump tool carries its own
// procedure for the one thing that writes.
type Client struct {
	// BaseURL is the API root.
	BaseURL string
	// Token is the bearer token sent with every request. It may be empty:
	// the repositories are public, and an unauthenticated read is enough
	// for a report of a dozen of them.
	Token string
	// HTTP is the client the requests are made with.
	HTTP *http.Client
}

// NewClient returns a client for the GitHub API.
func NewClient(token string) *Client {
	return &Client{
		BaseURL: APIBase,
		Token:   token,
		HTTP:    NewHTTPClient(),
	}
}

// Repos returns the repositories the owner owns.
//
// The authenticated listing is used when there is a token, because it also
// carries the private repositories; the public listing is the fallback.
func (c *Client) Repos(ctx context.Context, owner string) ([]Repo, error) {
	var raw []struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
	}
	path := "/user/repos?per_page=100&affiliation=owner"
	if c.Token == "" {
		path = "/users/" + url.PathEscape(owner) + "/repos?per_page=100"
	}
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{
			Name:          r.Name,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Archived:      r.Archived,
		})
	}
	return out, nil
}

// File reads one file of a repository at a ref.
func (c *Client) File(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	var raw struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	query := ""
	if ref != "" {
		query = "?ref=" + url.QueryEscape(ref)
	}
	if err := c.get(ctx, "/repos/"+owner+"/"+repo+"/contents/"+path+query, &raw); err != nil {
		return nil, err
	}
	if raw.Encoding != "base64" {
		return nil, fmt.Errorf("%s/%s: the API answered with %s content", owner, repo, raw.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw.Content, "\n", ""))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ListDir lists the entries of a repository directory at a ref.
func (c *Client) ListDir(ctx context.Context, owner, repo, path, ref string) ([]Entry, error) {
	var raw []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	query := ""
	if ref != "" {
		query = "?ref=" + url.QueryEscape(ref)
	}
	if err := c.get(ctx, "/repos/"+owner+"/"+repo+"/contents/"+path+query, &raw); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(raw))
	for _, e := range raw {
		out = append(out, Entry{Name: e.Name, Path: e.Path, Type: e.Type})
	}
	return out, nil
}

// HeadSHA returns the commit a ref points at.
func (c *Client) HeadSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	var raw struct {
		SHA string `json:"sha"`
	}
	if err := c.get(ctx, "/repos/"+owner+"/"+repo+"/commits/"+url.PathEscape(ref), &raw); err != nil {
		return "", err
	}
	return raw.SHA, nil
}

// CommitsAhead counts the commits a head has that a base does not.
//
// The comparison walks from the pinned commit to the head of the branch. The
// API reports the distance in ahead_by: the commits the head carries that the
// base does not, which is the number of commits the pin is behind by.
func (c *Client) CommitsAhead(ctx context.Context, owner, repo, base, head string) (int, error) {
	var raw struct {
		BehindBy int    `json:"behind_by"`
		AheadBy  int    `json:"ahead_by"`
		Status   string `json:"status"`
	}
	path := "/repos/" + owner + "/" + repo + "/compare/" + url.PathEscape(base) + "..." + url.PathEscape(head)
	if err := c.get(ctx, path, &raw); err != nil {
		return 0, err
	}
	return raw.AheadBy, nil
}

// get makes one request and decodes the answer.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}
