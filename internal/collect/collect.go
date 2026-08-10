// Package collect turns a directory of images into the grouped table model.
//
// One regular expression does the whole job. It is matched against each file's
// slash-separated path relative to the root, and its named captures decide
// where the image lands:
//
//	group  the table this image belongs to  (optional; absent -> one flat table)
//	row    the line within that table       (required)
//	col    the column within that line      (optional; absent -> ColDefault)
//
// Naming the three axes rather than hard-coding a directory layout is what lets
// the same action serve `<project>/<screen>-<theme>.png` and `<name>.png` alike.
package collect

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/miyamo2/contact-sheet/internal/sheet"
)

// DefaultLayout matches `<group>/<row>[-light|-dark].png`, the layout a
// Playwright project-per-viewport suite produces.
const DefaultLayout = `^(?P<group>[^/]+)/(?P<row>.+?)(?:-(?P<col>light|dark))?\.png$`

type Options struct {
	// Root is the directory to walk. A missing directory is not an error: it
	// yields no groups, which the caller reports as the "empty" state.
	Root   string
	Layout *regexp.Regexp
	// GroupOrder and ColOrder list names that sort first, in the order given.
	// Anything not listed follows, sorted lexically.
	GroupOrder []string
	ColOrder   []string
	// ColDefault names the column for images whose path has no `col` capture.
	// Empty means "the first entry of ColOrder".
	ColDefault string
}

type Result struct {
	Groups []sheet.Group
	// Paths lists every matched image, relative to Root and slash-separated, in
	// the order the groups present them. This is what gets committed.
	Paths []string
	Total int
}

// Collect walks Root and builds the model. Files that the layout does not match
// are skipped silently -- a captures directory holding a stray .gitkeep or a
// trace should not fail the run.
func Collect(o Options) (Result, error) {
	if o.Layout == nil {
		return Result{}, fmt.Errorf("collect: layout expression is required")
	}
	if err := requireCapture(o.Layout, "row"); err != nil {
		return Result{}, err
	}

	colDefault := o.ColDefault
	if colDefault == "" && len(o.ColOrder) > 0 {
		colDefault = o.ColOrder[0]
	}

	// group name -> row name -> column -> relative path
	tables := map[string]map[string]map[string]string{}
	seenCols := map[string]bool{}

	err := filepath.WalkDir(o.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// the root not existing is the caller's "no captures" case
			if path == o.Root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(o.Root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		match := o.Layout.FindStringSubmatch(rel)
		if match == nil {
			return nil
		}
		group := capture(o.Layout, match, "group")
		row := capture(o.Layout, match, "row")
		if row == "" {
			return nil
		}
		col := capture(o.Layout, match, "col")
		if col == "" {
			col = colDefault
		}
		if col == "" {
			return fmt.Errorf("collect: %s has no `col` capture and no col-default is set", rel)
		}

		seenCols[col] = true
		rows, ok := tables[group]
		if !ok {
			rows = map[string]map[string]string{}
			tables[group] = rows
		}
		cells, ok := rows[row]
		if !ok {
			cells = map[string]string{}
			rows[row] = cells
		}
		if previous, taken := cells[col]; taken {
			return fmt.Errorf("collect: %s and %s both land on %s/%s/%s", previous, rel, group, row, col)
		}
		cells[col] = rel
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	columns := order(keys(seenCols), o.ColOrder)
	result := Result{}
	for _, name := range order(keys(tables), o.GroupOrder) {
		group := sheet.Group{Name: name, Columns: columns}
		rowNames := keys(tables[name])
		sort.Strings(rowNames)
		for _, rowName := range rowNames {
			cells := tables[name][rowName]
			group.Rows = append(group.Rows, sheet.Row{Name: rowName, Cells: cells})
			for _, col := range columns {
				if path, ok := cells[col]; ok {
					result.Paths = append(result.Paths, path)
				}
			}
		}
		result.Groups = append(result.Groups, group)
	}
	result.Total = len(result.Paths)
	return result, nil
}

// order sorts names so that everything in first appears first, in that order,
// and the rest follows lexically.
func order(names, first []string) []string {
	rank := map[string]int{}
	for i, name := range first {
		rank[name] = i
	}
	sort.Slice(names, func(i, j int) bool {
		ri, oki := rank[names[i]]
		rj, okj := rank[names[j]]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		case okj:
			return false
		default:
			return names[i] < names[j]
		}
	})
	return names
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func capture(re *regexp.Regexp, match []string, name string) string {
	for i, n := range re.SubexpNames() {
		if n == name && i < len(match) {
			return match[i]
		}
	}
	return ""
}

func requireCapture(re *regexp.Regexp, name string) error {
	for _, n := range re.SubexpNames() {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("collect: the layout expression needs a (?P<%s>...) capture", name)
}
