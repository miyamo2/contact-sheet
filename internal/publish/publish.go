// Package publish puts the images somewhere a comment can load them from.
//
// A comment can only show an image from a public http(s) URL: GitHub's
// sanitiser drops `data:` sources, and an actions artifact is a zip behind a
// login. So the files have to be fetchable, and of the places to put them this
// is the only one that costs nothing:
//
//	a branch        `git fetch` takes refs/heads/* by default, so every clone
//	                and pull of the repository would carry every image
//	Git LFS         keeps clones small, but storage and bandwidth are metered
//	                and deleting the files does not give the quota back
//	release assets  free and unmetered, but a repository with no releases grows
//	                a Releases section that exists only to hold screenshots
//
// A ref outside refs/heads/* is in none of those ways visible: not in the
// branch list, not in the Releases tab, and not in the default fetch refspec
// (`+refs/heads/*:refs/remotes/origin/*`). GITHUB_TOKEN with contents: write may
// push such a ref, and raw serves a blob by commit sha without the commit being
// reachable from any branch.
//
// One ref per run, never rewritten, so a comment written months ago still
// resolves -- the ref is what keeps the objects from being collected.
package publish

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Options struct {
	// Root is the directory Paths are relative to.
	Root string
	// Paths are slash-separated paths under Root, and become the paths in the
	// commit -- so a URL is rawURL/repository/commit/path.
	Paths []string
	// Ref is the full ref to push to, e.g. refs/contact-sheet/pr-42/1234.1.
	Ref        string
	Repository string
	Token      string
	// ServerURL is where the push goes; GITHUB_SERVER_URL on GHES.
	ServerURL string
	Message   string
	Attempts  int
}

// Publish commits the images in a scratch repository -- the checkout the
// workflow is standing in is never touched -- and pushes that single parentless
// commit. Returns the commit sha the URLs will point at.
func Publish(ctx context.Context, o Options) (string, error) {
	if len(o.Paths) == 0 {
		return "", fmt.Errorf("publish: nothing to publish")
	}
	work, err := os.MkdirTemp("", "contact-sheet-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)

	for _, rel := range o.Paths {
		destination := filepath.Join(work, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := copyFile(filepath.Join(o.Root, filepath.FromSlash(rel)), destination); err != nil {
			return "", err
		}
	}

	steps := [][]string{
		{"init", "-q", "-b", "contact-sheet"},
		{"config", "user.name", "github-actions[bot]"},
		{"config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com"},
		// the runner has no signing key, and a signature would mean nothing here
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", o.Message},
	}
	for _, args := range steps {
		if _, err := git(ctx, work, args...); err != nil {
			return "", err
		}
	}

	remote := fmt.Sprintf("%s/%s.git", strings.TrimSuffix(o.ServerURL, "/"), o.Repository)
	remote = strings.Replace(remote, "://", "://x-access-token:"+o.Token+"@", 1)
	if _, err := git(ctx, work, "remote", "add", "origin", remote); err != nil {
		return "", err
	}

	attempts := o.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	// the one step that moves megabytes and can lose a connection
	for attempt := 1; ; attempt++ {
		_, err := git(ctx, work, "push", "origin", "HEAD:"+o.Ref)
		if err == nil {
			break
		}
		if attempt >= attempts {
			return "", err
		}
		select {
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	head, err := git(ctx, work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(head), nil
}

// git runs a command and returns stdout. The remote url carries the token, so
// no error here echoes the arguments; Actions masks GITHUB_TOKEN in logs either
// way, but a redacted message is not something to rely on the runner for.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	defer destination.Close()
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Close()
}
