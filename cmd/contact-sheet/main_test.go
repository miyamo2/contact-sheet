package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miyamo2/contact-sheet/internal/ghapi"
	"github.com/miyamo2/contact-sheet/internal/render"
	"github.com/miyamo2/contact-sheet/internal/sheet"
)

func TestLoadTemplateFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.tmpl")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadTemplate(context.Background(), http.DefaultClient, path)
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestLoadTemplateOverHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// nothing of the caller's should reach a third-party host
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("the request carried an Authorization header")
		}
		_, _ = w.Write([]byte("### {{ .Title }}"))
	}))
	defer server.Close()

	got, err := loadTemplate(context.Background(), server.Client(), server.URL+"/templates/gallery.tmpl")
	if err != nil {
		t.Fatalf("loadTemplate: %v", err)
	}
	if got != "### {{ .Title }}" {
		t.Errorf("got %q", got)
	}
}

// A 404 that becomes a comment body would post GitHub's error page to the pull
// request, so anything but 200 has to stop the run.
func TestLoadTemplateRejectsNotOK(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such file", http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := loadTemplate(context.Background(), server.Client(), server.URL+"/gone.tmpl"); err == nil {
		t.Fatal("want an error for a 404")
	}
}

// The body ends up in a comment on the caller's pull request; over plain HTTP
// anyone on the path could choose what it says.
func TestLoadTemplateRejectsPlainHTTP(t *testing.T) {
	_, err := loadTemplate(context.Background(), http.DefaultClient, "http://example.com/x.tmpl")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("want an https-only error, got %v", err)
	}
}

func TestLoadTemplateRejectsHugeBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, remoteTemplateLimit+1))
	}))
	defer server.Close()

	if _, err := loadTemplate(context.Background(), server.Client(), server.URL+"/big.tmpl"); err == nil {
		t.Fatal("want an error for a body over the limit")
	}
}

// Skipping is how a run says "the token I have cannot finish this", so the fork
// case turns on the flag rather than on the pull request alone -- and no flag
// makes a closed pull request worth commenting on.
func TestSkipReason(t *testing.T) {
	const base = "miyamo2/contact-sheet"

	pull := func(state, head string) *ghapi.PullRequest {
		p := &ghapi.PullRequest{Number: 7, State: state}
		p.Head.Repo.FullName = head
		p.Base.Repo.FullName = base
		return p
	}

	for _, tt := range []struct {
		name      string
		pull      *ghapi.PullRequest
		allowFork bool
		skip      bool
	}{
		{"open on a branch", pull("open", base), false, false},
		{"open from a fork", pull("open", "someone/contact-sheet"), false, true},
		{"open from a fork, allowed", pull("open", "someone/contact-sheet"), true, false},
		{"closed on a branch", pull("closed", base), false, true},
		{"closed from a fork, allowed", pull("closed", "someone/contact-sheet"), true, true},
		// the head repository is absent once a fork is deleted, and a pull
		// request on this repository's own branch is not a fork either way
		{"open with no head repository", pull("open", ""), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reason := skipReason(tt.pull, tt.allowFork)
			if got := reason != ""; got != tt.skip {
				t.Errorf("skipReason() = %q, want skip = %v", reason, tt.skip)
			}
		})
	}
}

// The key names the comment, so a remote template and a local copy of it have
// to land on the same one.
func TestTemplateKey(t *testing.T) {
	for ref, want := range map[string]string{
		"templates/gallery.tmpl":                                    "gallery",
		"./.github/contact-sheet.tmpl":                              "contact-sheet",
		"https://raw.githubusercontent.com/o/r/v1/tpl/gallery.tmpl": "gallery",
		"https://example.com/a/summary.tmpl?ref=v2":                 "summary",
	} {
		got, err := templateKey(ref)
		if err != nil {
			t.Errorf("templateKey(%q): %v", ref, err)
			continue
		}
		if got != want {
			t.Errorf("templateKey(%q) = %q, want %q", ref, got, want)
		}
	}
}

// Two entries landing on the same key would have one comment overwrite the
// other, which is worse than refusing to start.
func TestTemplatesRejectDuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, sub := range []string{a, b} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "sheet.tmpl"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config{templateFiles: filepath.Join(a, "sheet.tmpl") + "," + filepath.Join(b, "sheet.tmpl")}
	if _, err := templatesOf(context.Background(), cfg); err == nil {
		t.Fatal("want an error when two entries share a key")
	}
}

// The templates in this repository are advertised in the README and fetched by
// URL, so a broken one is broken for everyone who pointed at it. One subtest per
// file, so CI can name the template that failed rather than the directory.
func TestShippedTemplatesRender(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "templates", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no templates found")
	}

	images := []sheet.Image{
		withURL(sheet.NewImage("desktop/about-light.png", map[string]string{"screen": "about", "theme": "light"})),
		withURL(sheet.NewImage("desktop/about-dark.png", map[string]string{"screen": "about", "theme": "dark"})),
		withURL(sheet.NewImage("mobile/menu.png", map[string]string{"screen": "menu"})),
	}
	states := []render.Context{
		{State: render.StatePublished, Status: "success", Images: images, Total: len(images), Ref: "refs/contact-sheet/pr-1/1.1"},
		{State: render.StatePublishFailed, Status: "success", Total: 3, Failure: "remote hung up"},
		{State: render.StateEmpty, Status: "failure"},
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, ctx := range states {
				t.Run(string(ctx.State), func(t *testing.T) {
					renderer, err := render.New(file, string(raw), render.Options{ImageWidth: 360, Limit: 65536})
					if err != nil {
						t.Fatalf("parse: %v", err)
					}
					body, err := renderer.Render(ctx)
					if err != nil {
						t.Fatalf("render: %v", err)
					}
					if strings.TrimSpace(body) == "" {
						t.Error("rendered nothing")
					}
					if strings.Contains(body, "<no value>") {
						t.Errorf("unresolved field:\n%s", body)
					}
				})
			}
		})
	}
}

func withURL(i sheet.Image) sheet.Image {
	i.URL = "https://raw.example/" + i.Path
	return i
}

// collect lets a space, a `#` and anything outside ASCII through, none of which
// is a URL character. An unescaped one is a broken image at best, and at worst
// -- on a fork's pull request, where the file names are not ours -- a way out of
// the src attribute they are written into.
func TestWithURLsEscapesEachSegment(t *testing.T) {
	got := withURLs([]sheet.Image{
		sheet.NewImage("desktop chromium/about #1.png", nil),
		sheet.NewImage("日本語/ホーム.png", nil),
		sheet.NewImage("plain/about.png", nil),
	}, "https://raw.githubusercontent.com/", "o/r", "abc123")

	want := []string{
		"https://raw.githubusercontent.com/o/r/abc123/desktop%20chromium/about%20%231.png",
		"https://raw.githubusercontent.com/o/r/abc123/%E6%97%A5%E6%9C%AC%E8%AA%9E/%E3%83%9B%E3%83%BC%E3%83%A0.png",
		"https://raw.githubusercontent.com/o/r/abc123/plain/about.png",
	}
	for i, image := range got {
		if image.URL != want[i] {
			t.Errorf("%s\n got %s\nwant %s", image.Path, image.URL, want[i])
		}
		// the slashes between segments are path and stay slashes
		if strings.Count(image.URL, "/") != strings.Count(want[i], "/") {
			t.Errorf("%s: separators were escaped", image.Path)
		}
	}
}

// A skipped run is a green run, so the only thing that tells a workflow it did
// not comment is the state. It used to write no outputs at all, leaving a step
// that never ran and one that skipped indistinguishable.
func TestSkippedWritesTheState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", path)

	if err := skipped(13, 42); err != nil {
		t.Fatalf("skipped: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"state<<CONTACT_SHEET_EOF\nskipped\n",
		"total<<CONTACT_SHEET_EOF\n13\n",
		"pull<<CONTACT_SHEET_EOF\n42\n",
		"ref<<CONTACT_SHEET_EOF\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// An unescaped newline would end the workflow command and print the rest of the
// message as ordinary output, which is how an annotation goes missing.
func TestEscapeCommand(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"no comment on #42", "no comment on #42"},
		{"two\nlines", "two%0Alines"},
		{"100% of the time", "100%25 of the time"},
		{"carriage\r\nreturn", "carriage%0D%0Areturn"},
	} {
		if got := escapeCommand(tt.in); got != tt.want {
			t.Errorf("escapeCommand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSummarizeAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	summarize("### one")
	summarize("### two")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "### one\n### two\n" {
		t.Errorf("got %q", raw)
	}
}

// Outside Actions there is no summary file, and writing one is not something to
// fail a local --dry-run over.
func TestSummarizeWithoutActions(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	summarize("### nothing to write this to")
}

// The summary is what a maintainer reads when a fork's pull request got no
// comment, so it has to name the fix and not just the cause.
func TestForkSummaryNamesTheFix(t *testing.T) {
	got := fmt.Sprintf(forkSummary, 42)
	for _, want := range []string{"#42", "allow-fork: true", "read-only", "#pull-requests-from-forks"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary does not mention %q:\n%s", want, got)
		}
	}
}

// The other fork summary: allow-fork was set and the token could not write
// anyway. Naming the workflows that hold one matters more than naming the
// failure, because the reader has already decided they want the comment.
func TestForkTokenSummaryNamesWhereToMove(t *testing.T) {
	got := fmt.Sprintf(forkTokenSummary, 42, "miyamo2/blog")
	for _, want := range []string{"#42", "miyamo2/blog", "workflow_run", "issue_comment"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary does not mention %q:\n%s", want, got)
		}
	}
}

// git's stderr on a rejected push is several lines, and the default template
// renders Failure inside a code span that the first newline would end. What
// comes out has to be one line with no backtick in it, whatever went in.
func TestFailureTextFlattensGitStderr(t *testing.T) {
	err := errors.New("git push failed: remote: Permission to owner/repo.git denied.\n" +
		"fatal: unable to access `https://github.com/owner/repo.git/`: The requested URL returned error: 403")

	got := failureText(err)
	if strings.ContainsAny(got, "\n\r`") {
		t.Errorf("a newline or a backtick survived: %q", got)
	}
	for _, want := range []string{"Permission to owner/repo.git denied", "403"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q dropped %q", got, want)
		}
	}
}

// A remote answering with a page of `remote:` lines is not a comment.
func TestFailureTextIsCapped(t *testing.T) {
	got := failureText(errors.New(strings.Repeat("remote: no ", 500)))
	if n := len([]rune(got)); n > failureLimit+1 {
		t.Errorf("%d runes, want at most %d plus the ellipsis", n, failureLimit)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated message does not say so: %q", got)
	}
}

// git said nothing at all -- a signal, say. An empty code span reads as a bug in
// the action rather than as a failed push.
func TestFailureTextNeverRendersEmpty(t *testing.T) {
	if got := failureText(errors.New(" \n\t` `\n ")); got == "" {
		t.Error("failureText returned nothing to put in the comment")
	}
}

func TestOneLine(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string
		limit          int
	}{
		{name: "collapses runs", in: "a\n\n  b\tc", want: "a b c"},
		{name: "trims the ends", in: "  padded  ", want: "padded"},
		{name: "drops backticks", in: "unable to access `x`", want: "unable to access x"},
		{name: "drops control characters", in: "red\x1b[31mtext\x00", want: "red [31mtext"},
		{name: "leaves a short line alone", in: "remote hung up", want: "remote hung up", limit: 200},
		{name: "does not split a rune", in: strings.Repeat("あ", 10), want: "あああああ…", limit: 5},
		{name: "no cut on the space", in: "ab cd ef", want: "ab cd…", limit: 6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := oneLine(tt.in, tt.limit); got != tt.want {
				t.Errorf("oneLine(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}
