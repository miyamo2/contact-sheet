package collect

import (
	"path/filepath"
	"regexp"
	"testing"
)

// exampleLayout is one a user might write, not one the action ships: it names
// two captures the action attaches no meaning to.
const exampleLayout = `^(?:[^/]+/)?(?P<screen>.+?)(?:-(?P<theme>light|dark))?\.png$`

func compile(t *testing.T, layout string) *regexp.Regexp {
	t.Helper()
	re, err := Compile(layout)
	if err != nil {
		t.Fatalf("Compile(%q): %v", layout, err)
	}
	return re
}

// A layout's captures have to survive the walk: they are the only thing a
// template has to group by beyond the file's own path.
func TestCollectKeepsNamedCaptures(t *testing.T) {
	got, err := Collect(Options{
		Root:   filepath.Join("testdata", "captures"),
		Layout: compile(t, exampleLayout),
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 13 {
		t.Fatalf("total = %d, want 13", got.Total)
	}

	var found bool
	for _, image := range got.Images {
		if image.Path != "desktop-chromium/about-light.png" {
			continue
		}
		found = true
		if image.Dir != "desktop-chromium" {
			t.Errorf("dir = %q, want desktop-chromium", image.Dir)
		}
		if image.Match["screen"] != "about" {
			t.Errorf("screen = %q, want about", image.Match["screen"])
		}
		if image.Match["theme"] != "light" {
			t.Errorf("theme = %q, want light", image.Match["theme"])
		}
	}
	if !found {
		t.Fatal("desktop-chromium/about-light.png was not collected")
	}
}

// trace.zip sits in the same directory as the screenshots. Neither the layout
// nor the extension filter may let it through, or the comment shows a broken
// image where a trace is.
func TestCollectSkipsNonImages(t *testing.T) {
	for _, layout := range []string{exampleLayout, ""} {
		got, err := Collect(Options{
			Root:   filepath.Join("testdata", "captures"),
			Layout: compile(t, layout),
		})
		if err != nil {
			t.Fatalf("Collect(layout=%q): %v", layout, err)
		}
		for _, image := range got.Images {
			if filepath.Ext(image.Path) == ".zip" {
				t.Errorf("layout=%q collected %s", layout, image.Path)
			}
		}
		if got.Total != 13 {
			t.Errorf("layout=%q total = %d, want 13", layout, got.Total)
		}
	}
}

// No layout is the zero-configuration case: everything that looks like an image
// is collected, and Dir and Name alone are enough for a template to group by.
func TestCollectWithoutLayout(t *testing.T) {
	got, err := Collect(Options{Root: filepath.Join("testdata", "captures")})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 13 {
		t.Fatalf("total = %d, want 13", got.Total)
	}
	for _, image := range got.Images {
		if len(image.Match) != 0 {
			t.Errorf("%s carries captures %v with no layout", image.Path, image.Match)
		}
		if image.Dir == "" || image.Name == "" {
			t.Errorf("%s has dir=%q name=%q, want both set", image.Path, image.Dir, image.Name)
		}
	}
}

// Paths drive both the commit and the URLs, and the order drives the comment.
// Sorting here is what keeps two runs over the same directory identical.
func TestCollectSortsByPath(t *testing.T) {
	got, err := Collect(Options{
		Root:   filepath.Join("testdata", "captures"),
		Layout: compile(t, exampleLayout),
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got.Paths) != got.Total {
		t.Fatalf("paths = %d, total = %d", len(got.Paths), got.Total)
	}
	for i := 1; i < len(got.Paths); i++ {
		if got.Paths[i-1] >= got.Paths[i] {
			t.Fatalf("paths are not sorted: %q then %q", got.Paths[i-1], got.Paths[i])
		}
	}
}

func TestCollectMissingRootIsEmpty(t *testing.T) {
	got, err := Collect(Options{
		Root:   filepath.Join("testdata", "does-not-exist"),
		Layout: compile(t, exampleLayout),
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 0 || len(got.Images) != 0 {
		t.Errorf("got %d images, want none", got.Total)
	}
}

func TestCompileRejectsBadExpression(t *testing.T) {
	if _, err := Compile(`(?P<row>`); err == nil {
		t.Fatal("want an error for an unparseable layout")
	}
}
