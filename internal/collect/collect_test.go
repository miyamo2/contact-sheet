package collect

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

func options(t *testing.T, layout string) Options {
	t.Helper()
	return Options{
		Root:       filepath.Join("testdata", "captures"),
		Layout:     regexp.MustCompile(layout),
		GroupOrder: []string{"desktop-chromium", "mobile-chromium"},
		ColOrder:   []string{"light", "dark"},
	}
}

func TestCollectDefaultLayout(t *testing.T) {
	got, err := Collect(options(t, DefaultLayout))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(got.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(got.Groups))
	}
	// group-order wins over alphabetical, which would put mobile first
	if got.Groups[0].Name != "desktop-chromium" {
		t.Errorf("first group = %q, want desktop-chromium", got.Groups[0].Name)
	}
	// trace.zip does not match the layout and must not become a row
	if got.Total != 13 {
		t.Errorf("total = %d, want 13", got.Total)
	}
	if want := []string{"light", "dark"}; !equal(got.Groups[0].Columns, want) {
		t.Errorf("columns = %v, want %v", got.Groups[0].Columns, want)
	}
}

// A capture with no -light/-dark suffix is a light-mode shot: nothing toggles
// the theme before taking it. It must land in a cell, not in a column of its own.
func TestCollectUnsuffixedFallsToDefaultColumn(t *testing.T) {
	got, err := Collect(options(t, DefaultLayout))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	mobile := got.Groups[1]
	if mobile.Name != "mobile-chromium" {
		t.Fatalf("second group = %q, want mobile-chromium", mobile.Name)
	}
	row, ok := find(mobile.Rows, "menu-modal")
	if !ok {
		t.Fatal("menu-modal is missing")
	}
	if row.Cell("light") == "" {
		t.Error("menu-modal has no light cell")
	}
	if row.Cell("dark") != "" {
		t.Error("menu-modal grew a dark cell out of nowhere")
	}
}

// The row that only has one theme still has to render as a full-width line, so
// the missing cell is absent rather than an empty string that reads as an image.
func TestCollectMissingCellIsAbsent(t *testing.T) {
	got, err := Collect(options(t, DefaultLayout))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	row, _ := find(got.Groups[1].Rows, "menu-modal")
	if _, present := row.Cells["dark"]; present {
		t.Error("dark should be absent from Cells, not present and empty")
	}
}

// A layout with no `group` capture is the flat-directory case: one nameless
// table, which the default template renders without a <details> wrapper.
func TestCollectWithoutGroupCapture(t *testing.T) {
	o := options(t, `^(?P<row>.+?)(?:-(?P<col>light|dark))?\.png$`)
	o.Root = filepath.Join("testdata", "flat")
	o.GroupOrder = nil
	got, err := Collect(o)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(got.Groups))
	}
	if got.Groups[0].Name != "" {
		t.Errorf("group name = %q, want empty", got.Groups[0].Name)
	}
	if got.Total != 4 {
		t.Errorf("total = %d, want 4", got.Total)
	}
}

// Two files landing on the same group/row/column would silently drop one, so it
// is an error rather than a coin flip over which image the reviewer sees.
func TestCollectRejectsCollision(t *testing.T) {
	o := options(t, `^[^/]+/(?P<row>.+?)(?:-(?P<col>light|dark))?\.png$`)
	o.GroupOrder = nil
	if _, err := Collect(o); err == nil {
		t.Fatal("want an error: desktop and mobile both hold article-list-light.png")
	}
}

func TestCollectMissingRootIsEmpty(t *testing.T) {
	o := options(t, DefaultLayout)
	o.Root = filepath.Join("testdata", "does-not-exist")
	got, err := Collect(o)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 0 || len(got.Groups) != 0 {
		t.Errorf("got %d images in %d groups, want nothing", got.Total, len(got.Groups))
	}
}

func TestCollectRequiresRowCapture(t *testing.T) {
	o := options(t, `^(?P<group>[^/]+)/.+\.png$`)
	if _, err := Collect(o); err == nil {
		t.Fatal("want an error when the layout has no row capture")
	}
}

// Paths drive both the commit and the URLs, so every collected image has to be
// in the list exactly once.
func TestCollectPathsCoverEveryImage(t *testing.T) {
	got, err := Collect(options(t, DefaultLayout))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got.Paths) != got.Total {
		t.Fatalf("paths = %d, total = %d", len(got.Paths), got.Total)
	}
	seen := map[string]bool{}
	for _, path := range got.Paths {
		if seen[path] {
			t.Errorf("%s listed twice", path)
		}
		seen[path] = true
	}
}

func find(rows []sheet.Row, name string) (sheet.Row, bool) {
	for _, row := range rows {
		if row.Name == name {
			return row, true
		}
	}
	return sheet.Row{}, false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
