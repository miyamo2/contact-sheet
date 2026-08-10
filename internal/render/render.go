// Package render turns the collected model into the comment body.
//
// text/template, not html/template: the output is Markdown destined for a
// GitHub comment, and GitHub sanitises it on the way in. html/template would
// escape the URLs and tags this deliberately emits.
package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

//go:embed default.tmpl
var defaultTemplate string

// DefaultTemplate is the body used when no template-file is given. It is
// exported so `contact-sheet --print-template` can hand a user a starting point.
func DefaultTemplate() string { return defaultTemplate }

// State says which of the three outcomes the run reached. Every template has to
// account for all three, so it is a field rather than something inferred from
// an empty slice.
type State string

const (
	// StatePublished: images were collected and pushed; the URLs resolve.
	StatePublished State = "published"
	// StatePublishFailed: images were collected but the push failed. The URLs
	// would 404, so Groups is empty and Failure explains it.
	StatePublishFailed State = "publish-failed"
	// StateEmpty: the run produced no images at all.
	StateEmpty State = "empty"
)

type Run struct {
	ID      string
	Number  string
	Attempt string
	URL     string
}

type Pull struct {
	Number int
	URL    string
}

// Context is the value templates are executed against. It is the action's
// public API: fields may be added, but renaming or removing one is a breaking
// change.
type Context struct {
	State  State
	Status string // outcome of the job that produced the images: success | failure
	Title  string

	Repository string
	SHA        string
	ShortSHA   string
	CommitURL  string

	Run  Run
	Pull Pull

	// Ref and Commit locate the pushed images. Empty unless State is published.
	Ref    string
	Commit string

	Columns []string
	Groups  []sheet.Group
	Total   int

	// Failure carries the push error when State is publish-failed.
	Failure string

	// Omitted counts rows dropped to fit the comment length limit.
	Omitted int
}

// Succeeded reports whether the job that produced the images passed, so a
// template can branch without comparing strings.
func (c Context) Succeeded() bool { return c.Status == "success" }

// Published is shorthand for the common happy-path guard.
func (c Context) Published() bool { return c.State == StatePublished }

// Options tune the helpers the template calls.
type Options struct {
	// ImageWidth is the width attribute on every <img>. Zero omits it.
	ImageWidth int
	// RowLabel heads the first column of each table.
	RowLabel string
	// Limit caps the rendered body. GitHub rejects a comment over 65536
	// characters, so rows are dropped until it fits. Zero disables the cap.
	Limit int
}

// Renderer holds a parsed template and the helpers bound to it.
type Renderer struct {
	tmpl *template.Template
	opt  Options
}

// New parses text. name only appears in error messages.
func New(name, text string, opt Options) (*Renderer, error) {
	if opt.RowLabel == "" {
		opt.RowLabel = "name"
	}
	r := &Renderer{opt: opt}
	tmpl, err := template.New(name).Funcs(r.funcs()).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	r.tmpl = tmpl
	return r, nil
}

func (r *Renderer) funcs() template.FuncMap {
	return template.FuncMap{
		"table":   r.table,
		"img":     r.img,
		"details": details,
		"join":    strings.Join,
	}
}

// img renders one image cell, or an em dash when the URL is empty, so a table
// stays rectangular when a row is missing a column.
func (r *Renderer) img(url string) string {
	if url == "" {
		return "—"
	}
	if r.opt.ImageWidth <= 0 {
		return fmt.Sprintf("<img src=%q>", url)
	}
	return fmt.Sprintf("<img src=%q width=%q>", url, fmt.Sprint(r.opt.ImageWidth))
}

// table renders a group as a Markdown table, one column per group column.
func (r *Renderer) table(group sheet.Group) string {
	var b strings.Builder
	b.WriteString("| " + r.opt.RowLabel)
	for _, column := range group.Columns {
		b.WriteString(" | " + column)
	}
	b.WriteString(" |\n| ---")
	for range group.Columns {
		b.WriteString(" | ---")
	}
	b.WriteString(" |")
	for _, row := range group.Rows {
		b.WriteString("\n| `" + row.Name + "`")
		for _, column := range group.Columns {
			b.WriteString(" | " + r.img(row.Cell(column)))
		}
		b.WriteString(" |")
	}
	return b.String()
}

func details(summary, body string) string {
	return "<details>\n<summary>" + summary + "</summary>\n\n" + body + "\n\n</details>"
}

// Render executes the template, shrinking the model until the body fits the
// limit. It returns the body and the context actually rendered, whose Omitted
// count reflects any rows that were dropped.
func (r *Renderer) Render(ctx Context) (string, Context, error) {
	for {
		var buf bytes.Buffer
		if err := r.tmpl.Execute(&buf, ctx); err != nil {
			return "", ctx, fmt.Errorf("render: %w", err)
		}
		body := buf.String()
		if r.opt.Limit <= 0 || len(body) <= r.opt.Limit {
			return body, ctx, nil
		}
		dropped, ok := dropLastRow(ctx.Groups)
		if !ok {
			// nothing left to shed: the template itself is over the limit
			return "", ctx, fmt.Errorf(
				"render: body is %d characters with no images left to drop (limit %d)",
				len(body), r.opt.Limit)
		}
		ctx.Groups = dropped
		ctx.Omitted++
		ctx.Total = sheet.Total(dropped)
	}
}

// dropLastRow removes the final row of the last non-empty group, and the group
// itself once it is emptied. Trimming from the end keeps the first group -- the
// one group-order puts first -- intact for as long as possible.
func dropLastRow(groups []sheet.Group) ([]sheet.Group, bool) {
	for i := len(groups) - 1; i >= 0; i-- {
		if len(groups[i].Rows) == 0 {
			continue
		}
		out := make([]sheet.Group, len(groups))
		copy(out, groups)
		rows := make([]sheet.Row, len(out[i].Rows)-1)
		copy(rows, out[i].Rows[:len(out[i].Rows)-1])
		out[i].Rows = rows
		if len(rows) == 0 {
			out = append(out[:i], out[i+1:]...)
		}
		return out, true
	}
	return groups, false
}
