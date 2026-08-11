package publish

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bareRemote stands in for the repository the action pushes to. A local path
// exercises the real git plumbing -- parentless commit, ref outside refs/heads/*,
// blob reachable only through that ref -- with no network and no token.
func bareRemote(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "owner", "repo.git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	return root
}

func images(t *testing.T) (root string, paths []string) {
	t.Helper()
	root = t.TempDir()
	paths = []string{"desktop/home-light.png", "desktop/home-dark.png", "mobile/home-light.png"}
	for _, rel := range paths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("\x89PNG\r\n\x1a\n"+rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, paths
}

func options(t *testing.T) Options {
	t.Helper()
	root, paths := images(t)
	return Options{
		Root:       root,
		Paths:      paths,
		Ref:        "refs/contact-sheet/pr-42/1.1",
		Repository: "owner/repo",
		Token:      "unused-for-a-local-remote",
		ServerURL:  bareRemote(t),
		Message:    "contact sheet for #42",
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPublishPushesAnOrphanCommitToTheRef(t *testing.T) {
	o := options(t)
	remote := filepath.Join(o.ServerURL, "owner", "repo.git")

	commit, err := Publish(context.Background(), o)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(commit) != 40 {
		t.Fatalf("commit = %q, want a full sha", commit)
	}

	if got := gitOut(t, remote, "rev-parse", o.Ref); got != commit {
		t.Errorf("%s points at %s, want %s", o.Ref, got, commit)
	}
	// parentless: nothing about the repository's history comes along
	if parents := gitOut(t, remote, "rev-list", "--parents", "-n", "1", commit); parents != commit {
		t.Errorf("commit has parents: %q", parents)
	}
}

// The whole point of the ref namespace: `git fetch` takes refs/heads/* by
// default, so images pushed outside it never reach anyone's clone.
func TestPublishRefIsOutsideRefsHeads(t *testing.T) {
	o := options(t)
	remote := filepath.Join(o.ServerURL, "owner", "repo.git")

	if _, err := Publish(context.Background(), o); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if branches := gitOut(t, remote, "for-each-ref", "--format=%(refname)", "refs/heads/"); branches != "" {
		t.Errorf("the push created a branch: %q", branches)
	}

	clone := t.TempDir()
	gitOut(t, clone, "clone", "-q", remote, ".")
	if out := gitOut(t, clone, "for-each-ref", "--format=%(refname)"); strings.Contains(out, "contact-sheet") {
		t.Errorf("a default clone carried the images: %q", out)
	}
}

// Paths in the commit have to match the paths the URLs are built from, or every
// image in the comment 404s.
func TestPublishKeepsRelativePaths(t *testing.T) {
	o := options(t)
	remote := filepath.Join(o.ServerURL, "owner", "repo.git")

	commit, err := Publish(context.Background(), o)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	listed := strings.Split(gitOut(t, remote, "ls-tree", "-r", "--name-only", commit), "\n")
	want := map[string]bool{}
	for _, path := range o.Paths {
		want[path] = true
	}
	if len(listed) != len(want) {
		t.Fatalf("commit holds %v, want %v", listed, o.Paths)
	}
	for _, path := range listed {
		if !want[path] {
			t.Errorf("unexpected path in the commit: %q", path)
		}
	}
}

// Two runs on the same pull request write two refs, so a comment from an
// earlier run keeps resolving after a later one lands.
func TestPublishDoesNotDisturbAnEarlierRef(t *testing.T) {
	first := options(t)
	remote := filepath.Join(first.ServerURL, "owner", "repo.git")

	earlier, err := Publish(context.Background(), first)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	second := first
	second.Ref = "refs/contact-sheet/pr-42/1.2"
	if _, err := Publish(context.Background(), second); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// the earlier ref is what keeps an older comment's images from being
	// collected, so a later run must leave it exactly where it was
	if got := gitOut(t, remote, "rev-parse", first.Ref); got != earlier {
		t.Errorf("the earlier ref moved to %s, want %s", got, earlier)
	}
	// two runs with identical images legitimately produce the same commit; what
	// matters is that both refs exist and resolve
	for _, ref := range []string{first.Ref, second.Ref} {
		if got := gitOut(t, remote, "rev-parse", "--verify", ref); len(got) != 40 {
			t.Errorf("%s does not resolve: %q", ref, got)
		}
	}
}

func TestPublishRejectsAnEmptySet(t *testing.T) {
	o := options(t)
	o.Paths = nil
	if _, err := Publish(context.Background(), o); err == nil {
		t.Fatal("want an error when there is nothing to publish")
	}
}

// git prints a remote URL freely -- in `remote -v`, in an error, in the progress
// line -- so the one thing it must not hold is the token.
func TestRemoteURLCarriesNoCredentials(t *testing.T) {
	got := remoteURL("https://github.com/", "owner/repo")
	if want := "https://github.com/owner/repo.git"; got != want {
		t.Errorf("remoteURL = %q, want %q", got, want)
	}
}

// scratchRepo is an initialised repository standing in for the one Publish
// builds, so the config it writes can be read back with git itself.
func scratchRepo(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	if out, err := exec.Command("git", "-C", work, "init", "-q", "-b", "contact-sheet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return work
}

// The token goes in as a header on the scratch repository's config, scoped to
// the server. Reading it back with git is the part worth asserting: it proves
// the appended section is where git looks for it and parses as git config.
func TestWriteAuthConfigScopesTheHeaderToTheServer(t *testing.T) {
	work := scratchRepo(t)
	const token = "ghs_supersecrettokenvalue"

	if err := writeAuthConfig(work, "https://github.com", token); err != nil {
		t.Fatalf("writeAuthConfig: %v", err)
	}

	want := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	if got := gitOut(t, work, "config", "--get", "http.https://github.com/.extraheader"); got != want {
		t.Errorf("extraheader = %q, want %q", got, want)
	}
	// --get-urlmatch answers the question that matters: would git send this
	// header to the URL the push goes to
	remote := remoteURL("https://github.com", "owner/repo")
	if got := gitOut(t, work, "config", "--get-urlmatch", "http.extraheader", remote); got != want {
		t.Errorf("the header does not apply to %s: %q", remote, got)
	}
	// a header for github.com is not one to send to anywhere else
	if out, err := exec.Command("git", "-C", work, "config", "--get-urlmatch",
		"http.extraheader", "https://elsewhere.example/owner/repo.git").Output(); err == nil && len(out) > 0 {
		t.Errorf("the header would be sent to another host: %s", out)
	}
	// the config is the only copy, and the temp directory it is in goes away
	// with the run; the token still has no business sitting there in the clear
	config, err := os.ReadFile(filepath.Join(work, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), token) {
		t.Errorf("the token is in the config verbatim:\n%s", config)
	}
}

// The tests here, and anyone running the binary against a directory, push to a
// path rather than a URL. There is nothing to authenticate to.
func TestWriteAuthConfigSkipsANonHTTPRemote(t *testing.T) {
	work := scratchRepo(t)
	if err := writeAuthConfig(work, filepath.Join(t.TempDir(), "remote"), "ghs_token"); err != nil {
		t.Fatalf("writeAuthConfig: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(work, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "extraheader") {
		t.Errorf("wrote a credential for a local path:\n%s", config)
	}
}

// The push is the step that reaches the network, and a failed one is the step
// whose message reaches a pull request comment.
func TestPublishErrorDoesNotLeakTheToken(t *testing.T) {
	o := options(t)
	o.Token = "ghs_supersecrettokenvalue"
	o.ServerURL = filepath.Join(t.TempDir(), "nowhere")
	o.Attempts = 1

	_, err := Publish(context.Background(), o)
	if err == nil {
		t.Fatal("want a push failure against a remote that does not exist")
	}
	if strings.Contains(err.Error(), o.Token) {
		t.Errorf("the token reached the error message: %v", err)
	}
}
