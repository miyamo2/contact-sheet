// Package sheet holds the one model every stage of the run shares: a flat list
// of images.
//
// It is deliberately flat. An earlier version carried groups, rows and columns,
// which made "render a table" the only thing the action could do -- the shape of
// the output was decided in Go, and a template could only fill it in. Grouping
// is presentation, so it belongs to whoever writes the template; what the action
// owes them is each image's location and whatever the layout expression learned
// about it.
package sheet

import (
	"path"
	"strings"
)

// Image is one collected file. Path is the only field the publish stage needs;
// the rest exist so a template can decide where the image goes.
type Image struct {
	// Path is relative to the collected root and slash-separated. It is both
	// the path inside the pushed commit and the tail of URL.
	Path string
	// Dir is the directory part of Path, empty at the root. A capture-free
	// layout can still group by it.
	Dir string
	// Name is the file name without its extension, and Ext is that extension
	// without the dot.
	Name string
	Ext  string
	// URL is the raw URL of Path in the commit that holds it. Empty until the
	// push succeeds, which is why a template has to check State before using it.
	URL string
	// Match holds the layout expression's named captures for this file. A
	// capture that did not participate is present and empty.
	Match map[string]string
}

// NewImage fills the derived fields from a slash-separated relative path.
func NewImage(rel string, match map[string]string) Image {
	ext := strings.TrimPrefix(path.Ext(rel), ".")
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	return Image{
		Path:  rel,
		Dir:   dir,
		Name:  strings.TrimSuffix(path.Base(rel), path.Ext(rel)),
		Ext:   ext,
		Match: match,
	}
}

// Field resolves a name against the built-in fields first and the layout's
// captures second, so a template says `groupBy .Images "dir"` and
// `groupBy .Images "theme"` the same way without knowing which is which.
// An unknown name yields "" rather than an error: a capture that only some
// files carry is a normal thing for a template to group by.
func (i Image) Field(name string) string {
	switch strings.ToLower(name) {
	case "path":
		return i.Path
	case "dir":
		return i.Dir
	case "name":
		return i.Name
	case "ext":
		return i.Ext
	case "url":
		return i.URL
	}
	return i.Match[name]
}
