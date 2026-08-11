package collect

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// imageBytes encodes a picture of the given format, so that what the tests
// write is what a screenshot step would write rather than a header this package
// happens to recognise.
//
// webp is the exception: the standard library encodes none, and only the RIFF
// header decides what the bytes are, so that one is written out by hand.
func imageBytes(t *testing.T, format string) []byte {
	t.Helper()
	if format == "webp" {
		// "RIFF", the length of what follows, then the WEBP form and a VP8L
		// chunk header
		return []byte("RIFF\x14\x00\x00\x00WEBPVP8L\x08\x00\x00\x00\x2f\x00\x00\x00\x00\x00\x00\x00")
	}

	canvas := image.NewRGBA(image.Rect(0, 0, 1, 1))
	canvas.Set(0, 0, color.RGBA{R: 0x22, G: 0x44, B: 0x88, A: 0xff})

	var out bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&out, canvas)
	case "jpeg":
		err = jpeg.Encode(&out, canvas, nil)
	case "gif":
		err = gif.Encode(&out, canvas, nil)
	default:
		t.Fatalf("no encoder for %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return out.Bytes()
}

// write puts content at root/name, creating the directories above it.
func write(t *testing.T, root, name string, content []byte) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
	return full
}

// reasonFor returns the reason the result gives for leaving a path out, or ""
// if it did not leave it out.
func reasonFor(got Result, rel string) string {
	for _, skip := range got.Skipped {
		if skip.Path == rel {
			return skip.Reason
		}
	}
	return ""
}

func collected(got Result, rel string) bool {
	for _, p := range got.Paths {
		if p == rel {
			return true
		}
	}
	return false
}

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

// A path is written into the comment as text, and on a pull request from a fork
// whoever opened it chose that path. One that would end the cell, the span or
// the tag it lands in is left out -- and reported, because unlike a file the
// layout did not match, it was meant to be collected.
func TestCollectSkipsPathsACommentCannotHold(t *testing.T) {
	root := t.TempDir()
	safe := []string{"plain.png", "a space.png", "日本語.png", "shot(1).png"}
	unsafe := []string{
		"broken|cell.png", // ends the table cell it sits in
		"<b>shout.png",    // opens a tag in the summary of a <details>
		"link[a].png",     // opens a Markdown link
		"quote\".png",     // ends an attribute of the <img>
		"tick`.png",       // ends the code span around a row label
		"back\\slash.png", // escapes whatever follows it
	}
	for _, name := range append(append([]string{}, safe...), unsafe...) {
		write(t, root, name, imageBytes(t, "png"))
	}

	for _, layout := range []string{"", `\.png$`} {
		got, err := Collect(Options{Root: root, Layout: compile(t, layout)})
		if err != nil {
			t.Fatalf("Collect(layout=%q): %v", layout, err)
		}
		if got.Total != len(safe) {
			t.Errorf("layout=%q: total = %d, want %d: %v", layout, got.Total, len(safe), got.Paths)
		}
		for _, image := range got.Images {
			if !Safe(image.Path) {
				t.Errorf("layout=%q collected %q", layout, image.Path)
			}
		}
		if len(got.Skipped) != len(unsafe) {
			t.Errorf("layout=%q: skipped %d, want %d: %q", layout, len(got.Skipped), len(unsafe), got.Skipped)
		}
	}
}

// A directory name reaches the comment the same way a file name does, through
// groupBy and the summary of a <details>.
func TestCollectSkipsUnsafeDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, root, "<b>shouting/plain.png", imageBytes(t, "png"))
	got, err := Collect(Options{Root: root})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 0 {
		t.Errorf("collected %v, want nothing", got.Paths)
	}
	if len(got.Skipped) != 1 {
		t.Errorf("skipped = %q, want the one file", got.Skipped)
	}
}

// A symlink is reported by WalkDir the way an ordinary file is, and copying one
// follows it: whatever it points at is what would be committed and pushed to a
// public ref. The extension it wears is no evidence about the target.
func TestCollectSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("a token, say"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, root, "real.png", imageBytes(t, "png"))
	// one link to a file outside the tree, and one to a picture inside it: the
	// link is refused for being a link, not for where it happens to point
	if err := os.Symlink(outside, filepath.Join(root, "leaked.png")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real.png"), filepath.Join(root, "alias.png")); err != nil {
		t.Fatal(err)
	}

	for _, layout := range []string{"", `\.png$`} {
		got, err := Collect(Options{Root: root, Layout: compile(t, layout)})
		if err != nil {
			t.Fatalf("Collect(layout=%q): %v", layout, err)
		}
		if !collected(got, "real.png") {
			t.Errorf("layout=%q did not collect the one real image: %v", layout, got.Paths)
		}
		for _, rel := range []string{"leaked.png", "alias.png"} {
			if collected(got, rel) {
				t.Errorf("layout=%q collected the symlink %s", layout, rel)
			}
			if reason := reasonFor(got, rel); reason != reasonSymlink {
				t.Errorf("layout=%q: %s reported as %q, want %q", layout, rel, reason, reasonSymlink)
			}
		}
	}
}

// A directory reached through a symlink is not walked either -- WalkDir does not
// follow one -- so nothing under it is collected.
func TestCollectDoesNotFollowLinkedDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "elsewhere.png", imageBytes(t, "png"))
	if err := os.Symlink(outside, filepath.Join(root, "captures")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	got, err := Collect(Options{Root: root})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 0 {
		t.Errorf("collected %v through a linked directory, want nothing", got.Paths)
	}
}

// A name is not evidence of what a file holds, and on a pull request from a fork
// the name is the contributor's to choose. Two megabytes of anything called
// .png would be copied into the scratch repository, pushed to a public ref, and
// rendered as a broken cell.
func TestCollectSkipsContentThatIsNotAnImage(t *testing.T) {
	root := t.TempDir()
	write(t, root, "real.png", imageBytes(t, "png"))
	write(t, root, "secret.png", []byte(strings.Repeat("not a picture, a text file\n", 64)))
	write(t, root, "empty.png", nil)
	// a real picture, but not the one the name promises
	write(t, root, "mislabelled.png", imageBytes(t, "jpeg"))

	for _, layout := range []string{"", `\.png$`} {
		got, err := Collect(Options{Root: root, Layout: compile(t, layout)})
		if err != nil {
			t.Fatalf("Collect(layout=%q): %v", layout, err)
		}
		if got.Total != 1 || !collected(got, "real.png") {
			t.Errorf("layout=%q collected %v, want just real.png", layout, got.Paths)
		}
		for _, rel := range []string{"secret.png", "empty.png", "mislabelled.png"} {
			if reason := reasonFor(got, rel); !strings.Contains(reason, ".png file") {
				t.Errorf("layout=%q: %s reported as %q, want the extension named", layout, rel, reason)
			}
		}
	}
}

// jpg and jpeg are the same picture under two names, and webp is the one format
// this package names that the standard library cannot encode -- so every format
// the action claims to collect is checked against a file rather than assumed.
func TestCollectAcceptsEveryFormatItNames(t *testing.T) {
	root := t.TempDir()
	for name, format := range map[string]string{
		"shot.png":  "png",
		"shot.jpg":  "jpeg",
		"shot.jpeg": "jpeg",
		"shot.gif":  "gif",
		"shot.webp": "webp",
	} {
		write(t, root, name, imageBytes(t, format))
	}
	// and one the extension list leaves out on purpose
	write(t, root, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))

	got, err := Collect(Options{Root: root})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 5 {
		t.Errorf("collected %v, want the five images", got.Paths)
	}
	if len(got.Skipped) != 0 {
		t.Errorf("skipped %v, want nothing: the svg is not an image extension at all", got.Skipped)
	}
}

// A layout matches on the path, so it can pick a file whose extension this
// package knows nothing about. The bytes still have to be a picture, because a
// comment showing them has no extension to go on either.
func TestCollectChecksContentUnderAnUnknownExtension(t *testing.T) {
	root := t.TempDir()
	write(t, root, "shot.capture", imageBytes(t, "png"))
	write(t, root, "trace.capture", []byte(strings.Repeat("timeline\n", 64)))

	got, err := Collect(Options{Root: root, Layout: compile(t, `\.capture$`)})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.Total != 1 || !collected(got, "shot.capture") {
		t.Errorf("collected %v, want just shot.capture", got.Paths)
	}
	if reason := reasonFor(got, "trace.capture"); !strings.Contains(reason, "not an image") {
		t.Errorf("trace.capture reported as %q, want it named as not an image", reason)
	}
}

// A file the layout never matched is not the caller's problem and stays out of
// the report; one that was meant to be collected is named.
func TestCollectReportsOnlyFilesItMeantToCollect(t *testing.T) {
	root := t.TempDir()
	write(t, root, "notes.txt", []byte("nothing to do with the sheet"))
	write(t, root, "trace.zip", []byte("PK\x03\x04not really a zip"))
	write(t, root, "huge.png", []byte(strings.Repeat("x", 1024)))

	got, err := Collect(Options{Root: root})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].Path != "huge.png" {
		t.Errorf("skipped %v, want only huge.png", got.Skipped)
	}
}

func TestSafe(t *testing.T) {
	for _, tt := range []struct {
		rel  string
		want bool
	}{
		{"desktop-chromium/about-light.png", true},
		{"a space.png", true},
		{"日本語.png", true},
		{"under_score-and.dots.png", true},
		{"new\nline.png", false},
		{"bell\a.png", false},
		{"\xff\xfe.png", false},
	} {
		if got := Safe(tt.rel); got != tt.want {
			t.Errorf("Safe(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}
