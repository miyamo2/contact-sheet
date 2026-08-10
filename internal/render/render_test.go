package render

import (
	"strings"
	"testing"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

func image(path string, match map[string]string) sheet.Image {
	i := sheet.NewImage(path, match)
	i.URL = "https://raw.example/" + path
	return i
}

func published(images ...sheet.Image) Context {
	return Context{
		State:      StatePublished,
		Status:     "success",
		Title:      "Contact Sheet",
		Repository: "acme/app",
		Ref:        "refs/contact-sheet/pr-1/1.1",
		Run:        Run{Number: "1", URL: "https://example/run"},
		Images:     images,
		Total:      len(images),
	}
}

func render(t *testing.T, text string, ctx Context, opt Options) string {
	t.Helper()
	r, err := New("test", text, opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, err := r.Render(ctx)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return body
}

// The table helper is the one place a template can lean on for the common
// shape, so a row missing one column still has to come out rectangular --
// otherwise Markdown drops the row rather than the cell.
func TestTableFillsMissingCells(t *testing.T) {
	ctx := published(
		image("about-light.png", map[string]string{"screen": "about", "theme": "light"}),
		image("about-dark.png", map[string]string{"screen": "about", "theme": "dark"}),
		image("menu.png", map[string]string{"screen": "menu", "theme": "light"}),
	)
	body := render(t, `{{ Table .Images "screen" "theme" "light,dark" "light" }}`, ctx, Options{RowLabel: "screen"})

	if !strings.Contains(body, "| screen | light | dark |") {
		t.Errorf("header missing or misordered:\n%s", body)
	}
	menu := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| `menu`") {
			menu = line
		}
	}
	if menu == "" {
		t.Fatalf("no row for menu:\n%s", body)
	}
	if strings.Count(menu, "|") != 4 {
		t.Errorf("menu row is not rectangular: %s", menu)
	}
	if !strings.HasSuffix(menu, "— |") {
		t.Errorf("missing dark cell should be an em dash: %s", menu)
	}
}

// colOrder is what puts light before dark; without it the columns would come
// out in whatever order the files were walked in.
func TestTableOrdersColumns(t *testing.T) {
	ctx := published(
		image("a-dark.png", map[string]string{"screen": "a", "theme": "dark"}),
		image("a-light.png", map[string]string{"screen": "a", "theme": "light"}),
	)
	body := render(t, `{{ Table .Images "screen" "theme" "light,dark" "light" }}`, ctx, Options{})
	if !strings.Contains(body, "| name | light | dark |") {
		t.Errorf("want light before dark:\n%s", body)
	}
}

// One column is the default shape: an empty colField sends every image to
// colDefault, which is what heads that column.
func TestTableWithOneColumn(t *testing.T) {
	ctx := published(
		image("latency.png", nil),
		image("revenue.png", nil),
	)
	body := render(t, `{{ Table .Images "name" "" "" "image" }}`, ctx, Options{RowLabel: "file name"})
	if !strings.Contains(body, "| file name | image |") {
		t.Errorf("want a single named column:\n%s", body)
	}
	if strings.Count(body, "<img") != 2 {
		t.Errorf("want both images:\n%s", body)
	}
}

// groupBy is what replaced the group capture: any field, including the built-in
// Dir, splits the images without collect knowing anything about it.
func TestGroupByAnyField(t *testing.T) {
	ctx := published(
		image("desktop/a.png", map[string]string{"screen": "a"}),
		image("mobile/a.png", map[string]string{"screen": "a"}),
		image("mobile/b.png", map[string]string{"screen": "b"}),
	)
	body := render(t, `{{ range GroupBy .Images "dir" }}{{ .Key }}={{ len .Images }} {{ end }}`, ctx, Options{})
	if strings.TrimSpace(body) != "desktop=1 mobile=2" {
		t.Errorf("got %q", strings.TrimSpace(body))
	}
}

// A capture the layout never named has to read as empty rather than blow up,
// because a template written for one suite gets pointed at another.
func TestFieldOfUnknownName(t *testing.T) {
	ctx := published(image("a.png", nil))
	body := render(t, `[{{ Field (index .Images 0) "nope" }}]`, ctx, Options{})
	if body != "[]" {
		t.Errorf("got %q, want []", body)
	}
}

// GitHub rejects a body over 65536 characters outright. The action cannot know
// which images matter, so it says which template overflowed and stops, rather
// than trimming rows nobody asked it to trim.
func TestRenderRefusesToTrim(t *testing.T) {
	ctx := published(image("a.png", nil))
	r, err := New("big.tmpl", `{{ range .Images }}{{ .URL }}{{ end }}`, Options{Limit: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.Render(ctx)
	if err == nil {
		t.Fatal("want an error when the body is over the limit")
	}
	if !strings.Contains(err.Error(), "big.tmpl") {
		t.Errorf("error should name the template: %v", err)
	}
	if !strings.Contains(err.Error(), "split") {
		t.Errorf("error should say how to fix it: %v", err)
	}
}

// A failed push must not leave the reader with URLs that 404, so the built-in
// template has to say where the images actually are.
func TestDefaultTemplateOnPublishFailure(t *testing.T) {
	ctx := Context{
		State:   StatePublishFailed,
		Status:  "success",
		Total:   4,
		Failure: "remote hung up",
		Run:     Run{Number: "7", URL: "https://example/run/7"},
	}
	body := render(t, DefaultTemplate(), ctx, Options{})
	if !strings.Contains(body, "remote hung up") {
		t.Errorf("body should carry the failure:\n%s", body)
	}
	if strings.Contains(body, "<img") {
		t.Errorf("body should render no images:\n%s", body)
	}
}

// The built-in template is a folded section per directory and nothing else: no
// capture names, so it renders whatever the layout collected.
func TestDefaultTemplateNeedsNoCaptures(t *testing.T) {
	ctx := published(
		image("desktop/a.png", nil),
		image("mobile/b.png", nil),
	)
	body := render(t, DefaultTemplate(), ctx, Options{RowLabel: "file name"})
	for _, want := range []string{"<b>desktop</b>", "<b>mobile</b>", "| file name | image |", "<details>"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
}

// The status line reports the job that produced the images, which is not the
// same thing as whether they were published.
func TestDefaultTemplateStatusIsTheJob(t *testing.T) {
	ctx := published(image("desktop/a.png", nil))
	ctx.Status = "failure"
	body := render(t, DefaultTemplate(), ctx, Options{})
	if !strings.Contains(body, "❌ failed") {
		t.Errorf("want the failed marker:\n%s", body)
	}
	if !strings.Contains(body, "<img") {
		t.Errorf("images were published and should render:\n%s", body)
	}
}
