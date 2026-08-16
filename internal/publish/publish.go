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
// resolves: the ref is what keeps git from collecting the objects.
package publish

import (
	"context"
	"encoding/base64"
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
	// commit, so a URL is rawURL/repository/commit/path.
	Paths []string
	// Ref is the full ref to push to, e.g. refs/contact-sheet/pr-42/1234.1.
	Ref        string
	Repository string
	// Token authenticates the push. It never reaches the remote URL or a
	// command line; see writeAuthConfig.
	Token string
	// ServerURL is where the push goes; GITHUB_SERVER_URL on GHES.
	ServerURL string
	Message   string
	Attempts  int
}

// Publish commits the images in a scratch repository, leaving the checkout the
// workflow is standing in untouched, and pushes that single parentless commit.
// Returns the commit sha the URLs will point at.
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

	if err := writeAuthConfig(work, o.ServerURL, o.Token); err != nil {
		return "", err
	}
	if _, err := git(ctx, work, "remote", "add", "origin", remoteURL(o.ServerURL, o.Repository)); err != nil {
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

// remoteURL is the address the push goes to, and it holds no credential: the
// token travels in a header this package writes into the scratch repository's
// config instead. A URL with nothing secret in it is one git may print, echo
// into an error, or leave in a remote's configuration.
func remoteURL(serverURL, repository string) string {
	return fmt.Sprintf("%s/%s.git", strings.TrimSuffix(serverURL, "/"), repository)
}

// writeAuthConfig hands git the token by a route nothing else on the machine can
// read it from. In the remote URL it would be one `git remote -v` or one
// anonymise-me-if-you-please error message away from the outside; as `git -c
// http.extraheader=…` or `git config <key> <value>` it would sit in a command
// line for as long as the process lives, and /proc shows a command line to every
// process on the runner. That leaves the file: the config of a repository in a
// private temporary directory, which the deferred RemoveAll takes away with
// everything else.
//
// The header is scoped to the server it authenticates to, so a redirect
// elsewhere does not carry it. A remote that is not http(s), a local path as
// the tests push to, takes no credential at all.
func writeAuthConfig(work, serverURL, token string) error {
	base := strings.TrimSuffix(serverURL, "/")
	overHTTP := strings.HasPrefix(base, "https://") || strings.HasPrefix(base, "http://")
	if token == "" || !overHTTP {
		return nil
	}
	// basic auth in a header rather than credentials in the URL, the form
	// actions/checkout uses. The value is base64 of an ASCII pair, so none of
	// git's config quoting applies to it.
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	section := fmt.Sprintf("\n[http %q]\n\textraheader = %s\n", base+"/", header)

	file, err := os.OpenFile(filepath.Join(work, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(file, section); err != nil {
		return err
	}
	return file.Close()
}

// git runs a command and returns stdout. stderr becomes the error: it is git's
// own words about what went wrong, and the caller shapes it before it goes
// anywhere a person reads. Nothing here carries the token, neither the
// arguments nor the remote URL, so a message that escapes the log is a message
// and nothing more.
//
// GIT_TERMINAL_PROMPT stops a rejected push from waiting on a username: with the
// credential out of the URL, git would otherwise have somewhere to ask.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
