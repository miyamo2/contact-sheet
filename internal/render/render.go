// Package render turns the collected images into a comment body.
//
// text/template, not html/template: the output is Markdown destined for a
// GitHub comment, and GitHub sanitises it on the way in. html/template would
// escape the URLs and tags this deliberately emits.
//
// The template decides the shape of the comment. What this package supplies is
// the images, the run's identifiers, and helpers general enough to build a
// table, a list, or a paragraph with three pictures in it -- none of which the
// package prefers.
package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

//go:embed default.tmpl
var defaultTemplate string

// DefaultTemplate is the body used when no template file is given. It is
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
	// would 404, so Images is empty and Failure explains it.
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

	// Version is the contact-sheet build that wrote the comment, as the binary
	// reads it off its own build information -- a release tag, or a
	// pseudo-version and commit for a build off one.
	Version string

	Repository string
	SHA        string
	ShortSHA   string
	CommitURL  string

	Run  Run
	Pull Pull

	// Ref and Commit locate the pushed images. Empty unless State is published.
	Ref    string
	Commit string

	// Images is every collected image, sorted by path. Grouping and ordering
	// are the template's to do, with the helpers below.
	Images []sheet.Image
	Total  int

	// Failure carries the push error when State is publish-failed.
	Failure string
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
	// RowLabel heads the first column of a table built by the table helper.
	RowLabel string
	// Limit caps the rendered body. GitHub rejects a comment over 65536
	// characters; a body over the limit is an error the template author fixes
	// by splitting the template in two. Zero disables the check.
	Limit int
}

// Bucket is one result of groupBy: the shared value and the images that have it.
type Bucket struct {
	Key    string
	Images []sheet.Image
}

// Renderer holds a parsed template and the helpers bound to it.
type Renderer struct {
	name string
	tmpl *template.Template
	opt  Options
}

// New parses text. name identifies the template in error messages and, for the
// caller, in the comment marker.
func New(name, text string, opt Options) (*Renderer, error) {
	if opt.RowLabel == "" {
		opt.RowLabel = "name"
	}
	r := &Renderer{name: name, opt: opt}
	tmpl, err := template.New(name).Funcs(r.funcs()).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("render: %s: %w", name, err)
	}
	r.tmpl = tmpl
	return r, nil
}

func (r *Renderer) Name() string { return r.name }

func (r *Renderer) funcs() template.FuncMap {
	return template.FuncMap{
		"img":     r.img,
		"table":   r.table,
		"groupBy": groupBy,
		"orderBy": orderBy,
		"filter":  filter,
		"values":  values,
		"field":   func(image sheet.Image, name string) string { return image.Field(name) },
		"details": details,
		"split":   split,
		"join":    strings.Join,
	}
}

// img renders one image, or an em dash when there is nothing to render, so a
// table stays rectangular when a row is missing a column. It takes either an
// Image or a bare URL string, because a template that has already reached into
// a map has the URL and not the Image.
func (r *Renderer) img(v any) string {
	var url string
	switch value := v.(type) {
	case sheet.Image:
		url = value.URL
	case string:
		url = value
	}
	if url == "" {
		return "—"
	}
	if r.opt.ImageWidth <= 0 {
		return fmt.Sprintf("<img src=%q>", url)
	}
	return fmt.Sprintf("<img src=%q width=%q>", url, fmt.Sprint(r.opt.ImageWidth))
}

// table is a convenience, not the model. It lays images out with one row per
// distinct rowField and one column per distinct colField; an empty colField
// gives a single unnamed column. colOrder is a comma-separated list of column
// names to put first, and anything unlisted follows lexically. colDefault is
// the column for an image whose colField is empty, which an empty colField
// makes true of every image -- that is how one column gets a heading.
func (r *Renderer) table(images []sheet.Image, rowField, colField, colOrder, colDefault string) string {
	column := func(image sheet.Image) string {
		if value := image.Field(colField); value != "" {
			return value
		}
		return colDefault
	}

	seen := map[string]bool{}
	var names []string
	for _, image := range images {
		if name := column(image); !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	columns := orderBy(names, split(colOrder, ","))
	rows := values(images, rowField)

	cells := map[string]map[string]sheet.Image{}
	for _, image := range images {
		row := image.Field(rowField)
		if cells[row] == nil {
			cells[row] = map[string]sheet.Image{}
		}
		cells[row][column(image)] = image
	}

	var b strings.Builder
	b.WriteString("| " + r.opt.RowLabel)
	for _, column := range columns {
		b.WriteString(" | " + column)
	}
	b.WriteString(" |\n| ---")
	for range columns {
		b.WriteString(" | ---")
	}
	b.WriteString(" |")
	for _, row := range rows {
		b.WriteString("\n| `" + row + "`")
		for _, column := range columns {
			b.WriteString(" | " + r.img(cells[row][column]))
		}
		b.WriteString(" |")
	}
	return b.String()
}

// groupBy splits images by the value of one field, keeping the buckets in the
// order their first image appears -- which, since Collect sorts by path, is
// stable across runs.
func groupBy(images []sheet.Image, name string) []Bucket {
	var out []Bucket
	index := map[string]int{}
	for _, image := range images {
		key := image.Field(name)
		if at, ok := index[key]; ok {
			out[at].Images = append(out[at].Images, image)
			continue
		}
		index[key] = len(out)
		out = append(out, Bucket{Key: key, Images: []sheet.Image{image}})
	}
	return out
}

// orderBy sorts names so that everything in first appears first, in that order,
// and the rest follows lexically.
func orderBy(names, first []string) []string {
	rank := map[string]int{}
	for i, name := range first {
		rank[name] = i
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i]]
		rj, okj := rank[out[j]]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		case okj:
			return false
		default:
			return out[i] < out[j]
		}
	})
	return out
}

func filter(images []sheet.Image, name, value string) []sheet.Image {
	var out []sheet.Image
	for _, image := range images {
		if image.Field(name) == value {
			out = append(out, image)
		}
	}
	return out
}

// values lists the distinct values of a field, in first-appearance order.
func values(images []sheet.Image, name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, image := range images {
		value := image.Field(name)
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func details(summary, body string) string {
	return "<details>\n<summary>" + summary + "</summary>\n\n" + body + "\n\n</details>"
}

func split(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Render executes the template. A body over the limit is an error rather than
// something to silently trim: the action cannot know which images matter, and
// the template author can split one template into two.
func (r *Renderer) Render(ctx Context) (string, error) {
	var buf bytes.Buffer
	if err := r.tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render: %s: %w", r.name, err)
	}
	body := buf.String()
	// characters, not bytes: GitHub counts the limit in characters, and a
	// title or file name in a non-Latin script costs several bytes each.
	if n := utf8.RuneCountInString(body); r.opt.Limit > 0 && n > r.opt.Limit {
		return "", fmt.Errorf(
			"render: %s produced %d characters, over GitHub's %d limit for one comment; "+
				"split it into two template files",
			r.name, n, r.opt.Limit)
	}
	return body, nil
}
