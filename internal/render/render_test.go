package render

import (
	"strings"
	"testing"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

func groups(n int) []sheet.Group {
	rows := make([]sheet.Row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, sheet.Row{
			Name:  string(rune('a'+i%26)) + "-screen",
			Cells: map[string]string{"light": "https://example.test/light.png", "dark": "https://example.test/dark.png"},
		})
	}
	return []sheet.Group{{Name: "desktop", Columns: []string{"light", "dark"}, Rows: rows}}
}

func published(n int) Context {
	return Context{
		State:      StatePublished,
		Status:     "success",
		Title:      "Contact Sheet",
		Repository: "owner/repo",
		SHA:        strings.Repeat("a", 40),
		ShortSHA:   "aaaaaaa",
		Run:        Run{ID: "1", Number: "7", Attempt: "1", URL: "https://example.test/run"},
		Pull:       Pull{Number: 42, URL: "https://example.test/pr/42"},
		Ref:        "refs/contact-sheet/pr-42/1.1",
		Commit:     strings.Repeat("b", 40),
		Columns:    []string{"light", "dark"},
		Groups:     groups(n),
		Total:      n * 2,
	}
}

func render(t *testing.T, ctx Context, opt Options) (string, Context) {
	t.Helper()
	r, err := New("default", DefaultTemplate(), opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, out, err := r.Render(ctx)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return body, out
}

func TestDefaultTemplateParsesAndCoversEveryState(t *testing.T) {
	for _, state := range []State{StatePublished, StatePublishFailed, StateEmpty} {
		ctx := published(2)
		ctx.State = state
		if state != StatePublished {
			ctx.Groups = nil
			ctx.Failure = "push rejected"
		}
		body, _ := render(t, ctx, Options{ImageWidth: 360, RowLabel: "screen"})
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s rendered an empty body", state)
		}
		if strings.Contains(body, "<no value>") {
			t.Errorf("%s left an unresolved field:\n%s", state, body)
		}
	}
}

// A failed push must not leave the reader with URLs that 404; it has to say
// where the images actually are.
func TestPublishFailedPointsAtTheArtifacts(t *testing.T) {
	ctx := published(2)
	ctx.State = StatePublishFailed
	ctx.Groups = nil
	ctx.Failure = "push rejected"
	body, _ := render(t, ctx, Options{})
	if !strings.Contains(body, "push rejected") {
		t.Error("the reason is missing")
	}
	if strings.Contains(body, "<img") {
		t.Error("a failed publish rendered image tags")
	}
}

// The status line reports the job that produced the images, which is not the
// same thing as whether the images were published.
func TestStatusIsIndependentOfState(t *testing.T) {
	ctx := published(1)
	ctx.Status = "failure"
	body, _ := render(t, ctx, Options{})
	if !strings.Contains(body, "failed") {
		t.Error("a failing job rendered as passing")
	}
	if !strings.Contains(body, "<img") {
		t.Error("a failing job should still show its captures")
	}
}

func TestMissingCellRendersAsDash(t *testing.T) {
	ctx := published(1)
	ctx.Groups[0].Rows[0].Cells = map[string]string{"light": "https://example.test/light.png"}
	body, _ := render(t, ctx, Options{ImageWidth: 360})
	if !strings.Contains(body, "| — |") {
		t.Errorf("a missing cell did not render as an em dash:\n%s", body)
	}
}

func TestImageWidthZeroOmitsTheAttribute(t *testing.T) {
	body, _ := render(t, published(1), Options{ImageWidth: 0})
	if strings.Contains(body, "width=") {
		t.Error("image-width 0 still emitted a width attribute")
	}
}

// GitHub rejects a body over 65536 characters outright, so the renderer has to
// shed rows rather than let the API call fail.
func TestRenderShedsRowsToFitTheLimit(t *testing.T) {
	ctx := published(400)
	body, out := render(t, ctx, Options{ImageWidth: 360, Limit: 8000})
	if len(body) > 8000 {
		t.Fatalf("body is %d characters, limit was 8000", len(body))
	}
	if out.Omitted == 0 {
		t.Error("rows were dropped but Omitted stayed at zero")
	}
	if out.Total >= ctx.Total {
		t.Error("Total was not adjusted to the rows that survived")
	}
	if !strings.Contains(body, "dropped") {
		t.Error("the body does not admit that rows are missing")
	}
}

func TestRenderUnderTheLimitDropsNothing(t *testing.T) {
	ctx := published(3)
	_, out := render(t, ctx, Options{ImageWidth: 360, Limit: 65536})
	if out.Omitted != 0 {
		t.Errorf("Omitted = %d, want 0", out.Omitted)
	}
	if out.Total != ctx.Total {
		t.Errorf("Total = %d, want %d", out.Total, ctx.Total)
	}
}

// A template that cannot fit even with every row gone is a template problem,
// and reporting it beats posting a truncated comment.
func TestRenderFailsWhenTheTemplateItselfIsTooLong(t *testing.T) {
	r, err := New("big", strings.Repeat("x", 200), Options{Limit: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := r.Render(published(1)); err == nil {
		t.Fatal("want an error when nothing can be dropped")
	}
}

func TestCustomTemplateSeesTheContext(t *testing.T) {
	r, err := New("custom", `{{ .Pull.Number }}:{{ len .Groups }}:{{ range .Groups }}{{ table . }}{{ end }}`, Options{RowLabel: "screen"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, _, err := r.Render(published(2))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(body, "42:1:") {
		t.Errorf("context fields did not reach the template: %q", body)
	}
	if !strings.Contains(body, "| screen | light | dark |") {
		t.Errorf("the table helper did not honour row-label:\n%s", body)
	}
}

func TestNewRejectsABrokenTemplate(t *testing.T) {
	if _, err := New("broken", "{{ .Unclosed", Options{}); err == nil {
		t.Fatal("want a parse error")
	}
}
