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
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/miyamo2/contact-sheet/internal/collect"
	"github.com/miyamo2/contact-sheet/internal/ghapi"
	"github.com/miyamo2/contact-sheet/internal/publish"
	"github.com/miyamo2/contact-sheet/internal/render"
	"github.com/miyamo2/contact-sheet/internal/sheet"
)

// commentLimit is GitHub's maximum comment body length.
const commentLimit = 65536

type config struct {
	path         string
	layout       string
	groupOrder   string
	colOrder     string
	colDefault   string
	templateFile string
	commentID    string
	title        string
	status       string
	refNamespace string
	rowLabel     string
	imageWidth   int
	pullNumber   int
	dryRun       bool
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

	layout, err := regexp.Compile(cfg.layout)
	if err != nil {
		return fmt.Errorf("--layout is not a valid expression: %w", err)
	}

	collected, err := collect.Collect(collect.Options{
		Root:       cfg.path,
		Layout:     layout,
		GroupOrder: splitList(cfg.groupOrder),
		ColOrder:   splitList(cfg.colOrder),
		ColDefault: cfg.colDefault,
	})
	if err != nil {
		return err
	}
	logf("collected %d images in %d groups", collected.Total, len(collected.Groups))

	repository := env("GITHUB_REPOSITORY", "")
	sha := env("GITHUB_SHA", strings.Repeat("0", 40))
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
			logf("%s belongs to no pull request; nothing to comment on", short(sha))
			return nil
		}
		if pull.State != "open" {
			logf("#%d is %s; leaving it alone", pull.Number, pull.State)
			return nil
		}
		if pull.FromFork() {
			// a fork's GITHUB_TOKEN is read-only: it can neither push the ref
			// nor write the comment
			logf("#%d comes from a fork; skipping", pull.Number)
			return nil
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
		Pull:    render.Pull{Number: pull.Number, URL: pull.HTMLURL},
		Columns: columnsOf(collected),
		Total:   collected.Total,
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
		view.Groups = withURLs(collected, rawURL, repository, view.Commit)
	}

	marker := "<!-- " + cfg.commentID + " -->"
	body, rendered, err := renderBody(cfg, view, len(marker)+1)
	if err != nil {
		return err
	}
	body = marker + "\n" + body

	if cfg.dryRun {
		fmt.Println(body)
		return nil
	}

	commentID, err := client.UpsertComment(ctx, pull.Number, marker, body)
	if err != nil {
		return err
	}
	logf("commented on #%d (%d)", pull.Number, commentID)

	return writeOutputs(map[string]string{
		"state":      string(rendered.State),
		"total":      strconv.Itoa(rendered.Total),
		"ref":        rendered.Ref,
		"commit":     rendered.Commit,
		"comment-id": strconv.FormatInt(commentID, 10),
		"pull":       strconv.Itoa(pull.Number),
	})
}

// renderBody parses the template -- the user's or the built-in one -- and
// executes it. reserved is the room the marker takes out of the limit.
func renderBody(cfg config, view render.Context, reserved int) (string, render.Context, error) {
	name, text := "default", render.DefaultTemplate()
	if cfg.templateFile != "" {
		raw, err := os.ReadFile(cfg.templateFile)
		if err != nil {
			return "", view, fmt.Errorf("--template-file: %w", err)
		}
		name, text = cfg.templateFile, string(raw)
	}
	renderer, err := render.New(name, text, render.Options{
		ImageWidth: cfg.imageWidth,
		RowLabel:   cfg.rowLabel,
		Limit:      commentLimit - reserved,
	})
	if err != nil {
		return "", view, err
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

func columnsOf(c collect.Result) []string {
	if len(c.Groups) == 0 {
		return nil
	}
	return c.Groups[0].Columns
}

// withURLs rewrites each cell from a repository-relative path to the raw URL of
// that path in the commit that now holds it.
func withURLs(c collect.Result, rawURL, repository, commit string) []sheet.Group {
	out := make([]sheet.Group, 0, len(c.Groups))
	for _, group := range c.Groups {
		rows := make([]sheet.Row, 0, len(group.Rows))
		for _, row := range group.Rows {
			cells := make(map[string]string, len(row.Cells))
			for column, path := range row.Cells {
				cells[column] = fmt.Sprintf("%s/%s/%s/%s", strings.TrimSuffix(rawURL, "/"), repository, commit, path)
			}
			rows = append(rows, sheet.Row{Name: row.Name, Cells: cells})
		}
		out = append(out, sheet.Group{Name: group.Name, Columns: group.Columns, Rows: rows})
	}
	return out
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
