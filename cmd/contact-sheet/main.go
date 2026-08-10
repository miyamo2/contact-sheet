// Command contact-sheet attaches artifact images to the pull request comment.
//
//	contact-sheet --path e2e/captures --status success
//
// Every flag falls back to an environment variable so the composite action can
// pass values without quoting a multi-line template into a shell command, and
// so a developer can run the same binary against a local directory.
//
// --dry-run pushes nothing, comments nothing, and prints the body it would have
// posted, which is how a template is checked without a token.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/miyamo2/contact-sheet/internal/collect"
	"github.com/miyamo2/contact-sheet/internal/ghapi"
	"github.com/miyamo2/contact-sheet/internal/publish"
	"github.com/miyamo2/contact-sheet/internal/render"
	"github.com/miyamo2/contact-sheet/internal/sheet"
)

// commentLimit is GitHub's maximum comment body length.
const commentLimit = 65536

type config struct {
	path          string
	layout        string
	templateFiles string
	commentID     string
	title         string
	status        string
	refNamespace  string
	rowLabel      string
	sha           string
	imageWidth    int
	pullNumber    int
	dryRun        bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "contact-sheet: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) > 1 && os.Args[1] == "--print-template" {
		fmt.Print(render.DefaultTemplate())
		return nil
	}

	cfg := parseFlags()
	if cfg.path == "" {
		return errors.New("--path is required")
	}

	layout, err := collect.Compile(cfg.layout)
	if err != nil {
		return err
	}

	templates, err := templatesOf(ctx, cfg)
	if err != nil {
		return err
	}

	collected, err := collect.Collect(collect.Options{Root: cfg.path, Layout: layout})
	if err != nil {
		return err
	}
	logf("collected %d images for %d template(s)", collected.Total, len(templates))
	if n := len(collected.Skipped); n > 0 {
		// naming a few is enough to recognise the pattern; a directory holding
		// thousands of them should not take the log with it
		shown := collected.Skipped
		if len(shown) > 5 {
			shown = shown[:5]
		}
		logf("skipped %d file(s) whose name cannot be written into a comment: %q", n, shown)
	}

	repository := env("GITHUB_REPOSITORY", "")
	// a workflow_run job stands on the default branch, so the commit the images
	// belong to is one only the workflow knows; GITHUB_SHA is right everywhere else
	sha := cfg.sha
	if sha == "" {
		sha = env("GITHUB_SHA", strings.Repeat("0", 40))
	}
	serverURL := env("GITHUB_SERVER_URL", "https://github.com")
	apiURL := env("GITHUB_API_URL", "https://api.github.com")
	rawURL := env("GITHUB_RAW_URL", "https://raw.githubusercontent.com")
	token := os.Getenv("GITHUB_TOKEN")

	runID := env("GITHUB_RUN_ID", "local")
	runAttempt := env("GITHUB_RUN_ATTEMPT", "1")
	// re-running a workflow keeps the run id and bumps the attempt; the ref needs both
	runKey := runID + "." + runAttempt

	var (
		client *ghapi.Client
		pull   *ghapi.PullRequest
	)
	if cfg.dryRun {
		pull = &ghapi.PullRequest{Number: cfg.pullNumber, State: "open"}
	} else {
		if token == "" {
			return errors.New("GITHUB_TOKEN is required")
		}
		if repository == "" {
			return errors.New("GITHUB_REPOSITORY is required")
		}
		client = ghapi.New(apiURL, token, repository)
		if pull, err = resolvePull(ctx, client, cfg.pullNumber, sha); err != nil {
			return err
		}
		if pull == nil {
			// a push to a branch with no pull request open on it. Expected
			// rather than wrong, so it gets no annotation
			logf("%s belongs to no pull request; nothing to comment on", short(sha))
			return skipped(collected.Total, 0)
		}
		if pull.State != "open" {
			logf("#%d is %s; leaving it alone", pull.Number, pull.State)
			return skipped(collected.Total, pull.Number)
		}
		if pull.FromFork() {
			// a fork's pull request is not out of bounds -- the token is. The
			// workflow the fork triggered holds a read-only one, which can
			// neither push the ref nor write the comment; the same pull request
			// commented on from a workflow_run job holds this repository's,
			// which can. See "Pull requests from forks" in the README
			writable, err := client.Writable(ctx)
			if err != nil {
				return err
			}
			if !writable {
				// this one is worth saying out loud. A green job that quietly
				// did nothing looks exactly like one that worked, and the log
				// of a job that passed is not somewhere anybody goes looking
				logf("#%d comes from a fork and this token cannot write to %s; skipping", pull.Number, repository)
				notice(fmt.Sprintf(
					"No comment on #%d: it comes from a fork, and the token this job holds cannot write to %s.",
					pull.Number, repository))
				summarize(fmt.Sprintf(forkSummary, pull.Number, repository))
				return skipped(collected.Total, pull.Number)
			}
			logf("#%d comes from a fork, and this token can write to %s", pull.Number, repository)
		}
	}

	view := render.Context{
		State:      render.StateEmpty,
		Status:     cfg.status,
		Title:      cfg.title,
		Repository: repository,
		SHA:        sha,
		ShortSHA:   short(sha),
		CommitURL:  fmt.Sprintf("%s/%s/commit/%s", serverURL, repository, sha),
		Run: render.Run{
			ID:      runID,
			Number:  env("GITHUB_RUN_NUMBER", runID),
			Attempt: runAttempt,
			URL:     fmt.Sprintf("%s/%s/actions/runs/%s", serverURL, repository, runID),
		},
		Pull:  render.Pull{Number: pull.Number, URL: pull.HTMLURL},
		Total: collected.Total,
	}

	if collected.Total > 0 {
		ref := fmt.Sprintf("%s/pr-%d/%s", strings.TrimSuffix(cfg.refNamespace, "/"), pull.Number, runKey)
		switch {
		case cfg.dryRun:
			view.State = render.StatePublished
			view.Ref = ref
			view.Commit = strings.Repeat("0", 40)
		case pull.Base.Repo.Private:
			// raw.githubusercontent.com hands a private repository a short-lived
			// token url, so a comment cannot load the image. Pushing the ref
			// would cost storage and show nothing.
			view.State = render.StatePublishFailed
			view.Failure = "private repository: raw URLs need a token, so images cannot render in a comment"
			logf("%s is private; skipping the push", repository)
		default:
			commit, publishErr := publish.Publish(ctx, publish.Options{
				Root:       cfg.path,
				Paths:      collected.Paths,
				Ref:        ref,
				Repository: repository,
				Token:      token,
				ServerURL:  serverURL,
				Message:    fmt.Sprintf("contact sheet for #%d (run %s)", pull.Number, runKey),
			})
			if publishErr != nil {
				// the images are still in the run's artifacts, and saying so
				// beats a red step with no comment at all
				view.State = render.StatePublishFailed
				view.Failure = publishErr.Error()
				logf("could not publish: %v", publishErr)
			} else {
				view.State = render.StatePublished
				view.Ref = ref
				view.Commit = commit
				logf("pushed %s", ref)
			}
		}
	}

	if view.State == render.StatePublished {
		view.Images = withURLs(collected.Images, rawURL, repository, view.Commit)
	}

	// One template, one comment. The template author decides how many comments a
	// run writes by how many files they list, which is why nothing here splits a
	// body: a body over the limit is theirs to divide.
	prefix := "<!-- " + cfg.commentID + ":"
	var ids []string
	keep := map[string]bool{}
	for _, t := range templates {
		marker := prefix + t.key + " -->"
		keep[marker] = true

		body, err := renderOne(cfg, t, view, len(marker)+1)
		if err != nil {
			return err
		}
		body = marker + "\n" + body

		if cfg.dryRun {
			fmt.Println(body)
			continue
		}
		commentID, err := client.UpsertComment(ctx, pull.Number, marker, body)
		if err != nil {
			return err
		}
		ids = append(ids, strconv.FormatInt(commentID, 10))
		logf("commented on #%d as %s (%d)", pull.Number, t.key, commentID)
	}

	// a template dropped from the list leaves a comment showing an older run
	if !cfg.dryRun {
		if pruned, err := client.PruneComments(ctx, pull.Number, prefix, keep); err != nil {
			return err
		} else if pruned > 0 {
			logf("deleted %d comment(s) from templates no longer listed", pruned)
		}
	}

	// written on a dry run too. Nothing here pushes or comments, and a rehearsal
	// that leaves out what a workflow reads afterwards is not one
	return writeOutputs(map[string]string{
		"state":    string(view.State),
		"total":    strconv.Itoa(view.Total),
		"ref":      view.Ref,
		"commit":   view.Commit,
		"comments": strings.Join(ids, ","),
		"pull":     strconv.Itoa(pull.Number),
	})
}

// namedTemplate is one template file and the key that identifies the comment it
// writes. The key comes from the file name so that reordering the list does not
// shuffle which comment gets rewritten.
type namedTemplate struct {
	key  string
	name string
	text string
}

func templatesOf(ctx context.Context, cfg config) ([]namedTemplate, error) {
	refs := splitList(cfg.templateFiles)
	if len(refs) == 0 {
		return []namedTemplate{{key: "default", name: "default", text: render.DefaultTemplate()}}, nil
	}
	out := make([]namedTemplate, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		text, err := loadTemplate(ctx, http.DefaultClient, ref)
		if err != nil {
			return nil, fmt.Errorf("--template-files: %w", err)
		}
		key, err := templateKey(ref)
		if err != nil {
			return nil, fmt.Errorf("--template-files: %w", err)
		}
		if seen[key] {
			return nil, fmt.Errorf(
				"--template-files: two entries are both named %q, so one comment would overwrite the other", key)
		}
		seen[key] = true
		out = append(out, namedTemplate{key: key, name: ref, text: text})
	}
	return out, nil
}

// remoteTemplateLimit caps a fetched body. A template has to render into a
// 65536-character comment, so anything approaching a megabyte is a wrong URL --
// an HTML error page, or a file that is not a template -- and reading it all
// before finding that out is what the cap prevents.
const remoteTemplateLimit = 1 << 20

// loadTemplate reads a template from disk, or over HTTPS when the entry is a
// URL. Nothing is sent with the request: GITHUB_TOKEN belongs to the repository
// running the action and has no business reaching a third-party host, so a
// template that needs authentication has to be checked out instead.
func loadTemplate(ctx context.Context, client *http.Client, ref string) (string, error) {
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		raw, err := os.ReadFile(ref)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	if strings.HasPrefix(ref, "http://") {
		// the body becomes a comment on your pull request; anyone on the path
		// could rewrite it in transit
		return "", fmt.Errorf("%s: templates must be fetched over https", ref)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", ref, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, remoteTemplateLimit+1))
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	if len(body) > remoteTemplateLimit {
		return "", fmt.Errorf("%s: over %d bytes; that is not a template", ref, remoteTemplateLimit)
	}
	return string(body), nil
}

// templateKey names the comment a template writes. It is the base name without
// its extension, for a path and a URL alike, so moving a template to a remote
// copy of itself keeps rewriting the same comment.
func templateKey(ref string) (string, error) {
	base := ref
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		parsed, err := url.Parse(ref)
		if err != nil {
			return "", fmt.Errorf("%s: %w", ref, err)
		}
		base = path.Base(parsed.Path)
	} else {
		base = filepath.Base(ref)
	}
	key := strings.TrimSuffix(base, filepath.Ext(base))
	if key == "" || key == "." || key == "/" {
		return "", fmt.Errorf("%s: no file name to take a comment key from", ref)
	}
	return key, nil
}

// renderOne executes one template. reserved is the room its marker takes out of
// the limit.
func renderOne(cfg config, t namedTemplate, view render.Context, reserved int) (string, error) {
	renderer, err := render.New(t.name, t.text, render.Options{
		ImageWidth: cfg.imageWidth,
		RowLabel:   cfg.rowLabel,
		Limit:      commentLimit - reserved,
	})
	if err != nil {
		return "", err
	}
	return renderer.Render(view)
}

// resolvePull takes the pull request from the number the workflow already knows
// (a pull_request event) or, failing that, from the commit (a push event).
func resolvePull(ctx context.Context, client *ghapi.Client, number int, sha string) (*ghapi.PullRequest, error) {
	if number > 0 {
		return client.Pull(ctx, number)
	}
	return client.PullForCommit(ctx, sha)
}

// withURLs fills in each image's URL now that a commit holds it. Every segment
// is escaped separately, the slashes between them being the only ones that are
// path: collect passes a space, a `#` and anything outside ASCII through, and
// none of the three survives being dropped into a URL as it stands.
func withURLs(images []sheet.Image, rawURL, repository, commit string) []sheet.Image {
	out := make([]sheet.Image, 0, len(images))
	for _, image := range images {
		segments := strings.Split(image.Path, "/")
		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}
		image.URL = fmt.Sprintf("%s/%s/%s/%s",
			strings.TrimSuffix(rawURL, "/"), repository, commit, strings.Join(segments, "/"))
		out = append(out, image)
	}
	return out
}

// forkSummary is the run summary for the one skip a maintainer is likely to
// have meant to avoid. It names the fix rather than only the cause, because
// somebody reading it has a pull request in front of them with no comment on it
// and no idea that a second workflow is what this takes.
const forkSummary = `### Contact Sheet

No comment on #%d: it comes from a fork, and the token this job holds cannot
write to %s.

A workflow that a fork's pull request triggered gets a read-only token whatever
the permissions block asks for, and no secret of yours is passed to it either.
Commenting on one takes a second workflow, off your default branch — see
[Pull requests from forks](https://github.com/miyamo2/contact-sheet#pull-requests-from-forks).
`

// skipped ends a run that will not comment, and says so in the outputs. A
// workflow branching on `state` would otherwise read the empty string and have
// to know that it means this. `skipped` is not one of the states a template
// sees: the run ends before there is anything to render.
func skipped(total, pull int) error {
	return writeOutputs(map[string]string{
		"state":    "skipped",
		"total":    strconv.Itoa(total),
		"pull":     strconv.Itoa(pull),
		"ref":      "",
		"commit":   "",
		"comments": "",
	})
}

// notice puts a line on the run's page and in its annotations, which is where
// somebody looks when a job is green and nothing happened.
func notice(message string) {
	fmt.Printf("::notice title=Contact Sheet::%s\n", escapeCommand(message))
}

// escapeCommand encodes the three characters a workflow command's data cannot
// carry. Anything unescaped here would end the command early and print the rest
// as ordinary output.
func escapeCommand(message string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(message)
}

// summarize appends Markdown to the run summary, the panel at the top of a
// run's page. Not being able to write it is not a reason to fail a run that
// otherwise did what it was asked.
func summarize(markdown string) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logf("could not write the run summary: %v", err)
		return
	}
	defer file.Close()
	if _, err := io.WriteString(file, markdown+"\n"); err != nil {
		logf("could not write the run summary: %v", err)
	}
}

func writeOutputs(values map[string]string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for key, value := range values {
		// heredoc form: a value may contain newlines
		if _, err := fmt.Fprintf(file, "%s<<CONTACT_SHEET_EOF\n%s\nCONTACT_SHEET_EOF\n", key, value); err != nil {
			return err
		}
	}
	return file.Close()
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func logf(format string, args ...any) {
	fmt.Printf("\x1b[36m[contact-sheet]\x1b[0m "+format+"\n", args...)
}
