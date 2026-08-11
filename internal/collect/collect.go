// Package collect walks a directory and returns the images in it.
//
// The layout expression is optional and does two things, neither of which is
// placement: it filters, and it extracts. A file the expression does not match
// is skipped, and the named captures it does match become that image's Match
// map for a template to group and order by. The names are the template author's
// to choose -- collect attaches no meaning to any of them.
//
// Without an expression every file whose extension looks like an image is
// collected, and a template still has Dir and Name to work with.
package collect

import (
	"errors"
	"fmt"
	"io/fs"
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
// it. SVG is absent on purpose: GitHub's image proxy does not render one from a
// raw URL, so collecting them would produce broken cells rather than pictures.
var imageExtensions = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true,
}

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
	// Skipped lists the images left out because Safe rejected their path, so
	// the caller can say so rather than have them go missing quietly.
	Skipped []string
}

// unsafeInPath is the set of characters a collected path may not contain. A
// path is written into the comment as text -- a table cell, a code span, the
// summary of a <details> -- and each of these ends the construct it was written
// into and begins another: a second cell, a tag, a link.
//
// It matters because the path is not the action's to choose. The images come
// out of a directory the workflow points at, and on a pull request from a fork
// that directory was filled by whoever opened it. Escaping instead would need
// to know where the template puts the name, which is the template author's
// business and not this package's.
const unsafeInPath = "`|<>\"\\[]"

// Safe reports whether a slash-separated relative path can be written into a
// comment body as it stands. Control characters and anything that is not valid
// UTF-8 are out for the same reason as the punctuation: what a comment does
// with them is not predictable enough to find out on someone's pull request.
func Safe(rel string) bool {
	return utf8.ValidString(rel) &&
		!strings.ContainsAny(rel, unsafeInPath) &&
		strings.IndexFunc(rel, unicode.IsControl) < 0
}

// Collect walks Root. Files that the layout does not match are skipped in
// silence -- a captures directory holding a stray .gitkeep or a trace should not
// fail the run -- and the images come back sorted by path so two runs over the
// same directory produce the same comment.
func Collect(o Options) (Result, error) {
	var (
		images  []sheet.Image
		skipped []string
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

		var captures map[string]string
		if o.Layout == nil {
			if !imageExtensions[strings.ToLower(strings.TrimPrefix(path.Ext(rel), "."))] {
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

		// the file is one to collect; whether its name survives being written
		// into a comment is the last thing between it and the sheet. Reported
		// rather than dropped in silence, because unlike a file the layout did
		// not match, this one was meant to be here
		if !Safe(rel) {
			skipped = append(skipped, rel)
			return nil
		}
		images = append(images, sheet.NewImage(rel, captures))
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Slice(images, func(i, j int) bool { return images[i].Path < images[j].Path })

	sort.Strings(skipped)

	result := Result{Images: images, Total: len(images), Skipped: skipped}
	for _, image := range images {
		result.Paths = append(result.Paths, image.Path)
	}
	return result, nil
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
