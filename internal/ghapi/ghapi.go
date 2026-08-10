// Package ghapi is a four-endpoint GitHub REST client.
//
// Deliberately not go-github: this reaches for pulls-for-a-commit and the issue
// comment CRUD and nothing else, and staying dependency-free keeps the release
// binaries reproducible from the standard library alone.
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string // https://api.github.com, or GITHUB_API_URL on GHES
	Token      string
	Repository string // owner/repo
	HTTP       *http.Client
}

func New(baseURL, token, repository string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		Token:      token,
		Repository: repository,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("%s %s -> %s: %s", method, path, res.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

type Repo struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

type PullRequest struct {
	Number  int    `json:"number"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Repo Repo `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo Repo `json:"repo"`
	} `json:"base"`
}

// FromFork reports whether the pull request's head lives in another repository.
// A fork's GITHUB_TOKEN is read-only, so the push stage cannot run for one.
func (p PullRequest) FromFork() bool {
	return p.Head.Repo.FullName != "" && p.Head.Repo.FullName != p.Base.Repo.FullName
}

// PullForCommit returns the pull request a commit belongs to. It prefers an
// open one, because a commit that also appears on a merged pull request should
// still comment on the branch being reviewed. Returns nil when there is none.
func (c *Client) PullForCommit(ctx context.Context, sha string) (*PullRequest, error) {
	var pulls []PullRequest
	path := fmt.Sprintf("/repos/%s/commits/%s/pulls", c.Repository, sha)
	if err := c.do(ctx, http.MethodGet, path, nil, &pulls); err != nil {
		return nil, err
	}
	for i := range pulls {
		if pulls[i].State == "open" {
			return &pulls[i], nil
		}
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	return &pulls[0], nil
}

// Pull fetches one pull request by number, for the pull_request events where
// the number is already known.
func (c *Client) Pull(ctx context.Context, number int) (*PullRequest, error) {
	var pull PullRequest
	path := fmt.Sprintf("/repos/%s/pulls/%d", c.Repository, number)
	if err := c.do(ctx, http.MethodGet, path, nil, &pull); err != nil {
		return nil, err
	}
	return &pull, nil
}

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// listComments pages through one pull request's issue comments.
func (c *Client) listComments(ctx context.Context, pull int) ([]comment, error) {
	var out []comment
	for page := 1; ; page++ {
		var comments []comment
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", c.Repository, pull, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &comments); err != nil {
			return nil, err
		}
		out = append(out, comments...)
		if len(comments) < 100 {
			return out, nil
		}
	}
}

// UpsertComment rewrites the comment whose body starts with marker, or posts a
// new one. Returns the comment's id.
func (c *Client) UpsertComment(ctx context.Context, pull int, marker, body string) (int64, error) {
	comments, err := c.listComments(ctx, pull)
	if err != nil {
		return 0, err
	}
	for _, existing := range comments {
		if strings.HasPrefix(existing.Body, marker) {
			path := "/repos/" + c.Repository + "/issues/comments/" + strconv.FormatInt(existing.ID, 10)
			return existing.ID, c.do(ctx, http.MethodPatch, path, map[string]string{"body": body}, nil)
		}
	}
	var created comment
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", c.Repository, pull)
	if err := c.do(ctx, http.MethodPost, path, map[string]string{"body": body}, &created); err != nil {
		return 0, err
	}
	return created.ID, nil
}

// PruneComments deletes the comments this action wrote under prefix whose
// marker is not in keep. Dropping a template from the list would otherwise
// leave its comment behind, showing a previous run's images forever.
func (c *Client) PruneComments(ctx context.Context, pull int, prefix string, keep map[string]bool) (int, error) {
	comments, err := c.listComments(ctx, pull)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, existing := range comments {
		if !strings.HasPrefix(existing.Body, prefix) {
			continue
		}
		marker := existing.Body
		if end := strings.IndexByte(marker, '\n'); end >= 0 {
			marker = marker[:end]
		}
		if keep[strings.TrimSpace(marker)] {
			continue
		}
		path := "/repos/" + c.Repository + "/issues/comments/" + strconv.FormatInt(existing.ID, 10)
		if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
