// Package collect walks a directory and returns the images in it.
//
// The layout expression is optional and does two things, neither of which is
// placement: it filters, and it extracts. A file the expression does not match
// is skipped, and the named captures it does match become that image's Match
// map for a template to group and order by. The names are the template author's
// to choose, and collect attaches no meaning to any of them.
//
// Without an expression it collects every file whose extension looks like an
// image, and a template still has Dir and Name to work with.
//
// collect takes neither the expression nor the extension at its word. A name is
// no evidence about contents, and on a pull request from a fork the names are
// the contributor's to choose, so a collected file also has to be a symlink-free
// regular file whose leading bytes are the picture its extension promises.
package collect

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

// imageExtensions is what counts as an image when no layout expression narrows
// it, mapped to the content type a file carrying that extension has to hold.
// SVG is absent on purpose: GitHub's image proxy does not render one from a raw
// URL, so collecting them would produce broken cells rather than pictures.
var imageExtensions = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
}

// imageContentTypes is the same set read from the other end: what a collected
// file's leading bytes may be. A layout matches on the path rather than the
// extension, so a file it picks need not carry an extension this package knows,
// but it does still have to be one of these pictures.
var imageContentTypes = func() map[string]bool {
	set := make(map[string]bool, len(imageExtensions))
	for _, contentType := range imageExtensions {
		set[contentType] = true
	}
	return set
}()

type Options struct {
	// Root is the directory to walk. A missing directory is not an error: it
	// yields no images, which the caller reports as the "empty" state.
	Root string
	// Layout filters and annotates. Nil collects every image file.
	Layout *regexp.Regexp
}

type Result struct {
	Images []sheet.Image
	// Paths lists every collected image, relative to Root and slash-separated.
	// This is what gets committed.
	Paths []string
	Total int
	// Skipped lists the files that the layout or the extension picked out and
	// something else then refused, so the caller can say so rather than let
	// them go missing in silence.
	Skipped []Skip
}

// Skip is one such file and the reason it was left out, written into the log as
// it stands.
type Skip struct {
	Path   string
	Reason string
}

const (
	reasonSymlink   = "a symbolic link"
	reasonIrregular = "not a regular file"
	reasonUnsafe    = "a name a comment cannot hold"
)

// unsafeInPath is the set of characters a collected path may not contain. A
// template writes a path into the comment as text, in a table cell, a code
// span, or the summary of a <details>, and each of those ends the construct
// holding it and begins another: a second cell, a tag, a link.
//
// It matters because the path is not the action's to choose. The images come
// out of a directory the workflow points at, and on a pull request from a fork
// that directory was filled by whoever opened it. Escaping instead would need
// to know where the template puts the name, which is the template author's
// business and not this package's.
const unsafeInPath = "`|<>\"\\[]"

// Safe reports whether a slash-separated relative path can be written into a
// comment body as it stands. Control characters and anything that is not valid
// UTF-8 are out for the same reason as the punctuation: a comment's handling of
// them is too unpredictable to find out on someone's pull request.
func Safe(rel string) bool {
	return utf8.ValidString(rel) &&
		!strings.ContainsAny(rel, unsafeInPath) &&
		strings.IndexFunc(rel, unicode.IsControl) < 0
}

// Collect walks Root. It skips the files the layout does not match in silence,
// since a captures directory holding a stray .gitkeep or a trace should not
// fail the run, and returns the images sorted by path so two runs over the
// same directory produce the same comment.
//
// A file the layout or the extension did pick still has to be a regular file,
// not a symlink, with a name a comment can hold and the bytes its extension
// promises. Those four go to Skipped rather than disappearing in silence.
func Collect(o Options) (Result, error) {
	var (
		images  []sheet.Image
		skipped []Skip
	)

	err := filepath.WalkDir(o.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// the root not existing is the caller's "no captures" case
			if p == o.Root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(o.Root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(rel), "."))

		var captures map[string]string
		if o.Layout == nil {
			if _, ok := imageExtensions[ext]; !ok {
				return nil
			}
		} else {
			match := o.Layout.FindStringSubmatch(rel)
			if match == nil {
				return nil
			}
			captures = map[string]string{}
			for i, name := range o.Layout.SubexpNames() {
				if name == "" || i >= len(match) {
					continue
				}
				captures[name] = match[i]
			}
		}

		// the file is one to collect, and what stands between it and the sheet
		// from here is what it is rather than what it is called. Each of these
		// is reported rather than dropped in silence, because unlike a file the
		// layout did not match, this one was meant to be here
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			// WalkDir reports a symlink the way it reports an ordinary file,
			// and copying one follows it: whatever it points at, anywhere in
			// the filesystem, is what would be committed and pushed to a public
			// ref
			skipped = append(skipped, Skip{Path: rel, Reason: reasonSymlink})
			return nil
		case !d.Type().IsRegular():
			// a device, a socket or a fifo. Reading one on a runner ranges from
			// pointless to a step that never returns
			skipped = append(skipped, Skip{Path: rel, Reason: reasonIrregular})
			return nil
		case !Safe(rel):
			skipped = append(skipped, Skip{Path: rel, Reason: reasonUnsafe})
			return nil
		}
		if reason := contentReason(p, ext); reason != "" {
			skipped = append(skipped, Skip{Path: rel, Reason: reason})
			return nil
		}
		images = append(images, sheet.NewImage(rel, captures))
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Slice(images, func(i, j int) bool { return images[i].Path < images[j].Path })

	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })

	result := Result{Images: images, Total: len(images), Skipped: skipped}
	for _, image := range images {
		result.Paths = append(result.Paths, image.Path)
	}
	return result, nil
}

// sniffLimit is how much of a file http.DetectContentType reads. Anything past
// it is decoration as far as identifying the file goes.
const sniffLimit = 512

// contentReason reports why the file at p is not the picture its extension
// promises, or "" when it is. A file that cannot be read gets a reason rather
// than an error: one image the runner cannot open is not a reason to fail a run
// that has a directory full of others, and the log names it either way.
//
// The stated extension is what the bytes are held against, because a name that
// says .png over bytes that say something else is the case worth catching: a
// comment renders what the bytes are, and a reviewer should not discover the
// mismatch by opening the link. Under a layout the extension may be one this
// package knows nothing about, and then it is enough that the bytes are an
// image at all.
func contentReason(p, ext string) string {
	got, err := detect(p)
	if err != nil {
		return fmt.Sprintf("unreadable: %v", err)
	}
	if want, known := imageExtensions[ext]; known {
		if got != want {
			return fmt.Sprintf("%s content in a .%s file", got, ext)
		}
		return ""
	}
	if !imageContentTypes[got] {
		return fmt.Sprintf("%s content, which is not an image", got)
	}
	return ""
}

// detect reads the leading bytes of a file and reports what they say it is.
// http.DetectContentType knows all four of the formats collected here, webp
// included, so this needs nothing beyond the standard library.
//
// Opening the file is only safe because the symlink is already out by the time
// the caller reaches this: os.Open follows one.
func detect(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()

	head := make([]byte, sniffLimit)
	n, err := io.ReadFull(file, head)
	// a picture shorter than the sniff limit is a short read, not a failure
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}

// Compile turns the layout input into an expression. An empty string means "no
// expression", which is not the same as one that matches everything: it also
// switches on the extension filter.
func Compile(layout string) (*regexp.Regexp, error) {
	if strings.TrimSpace(layout) == "" {
		return nil, nil
	}
	re, err := regexp.Compile(layout)
	if err != nil {
		return nil, fmt.Errorf("collect: layout expression: %w", err)
	}
	return re, nil
}
