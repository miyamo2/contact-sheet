// Package sheet holds the data model shared by every stage of the run.
//
// The shape is deliberately small: images are grouped, each group is a table of
// rows, and each row holds one cell per column. Everything else -- the wording,
// the ordering, whether a group becomes a <details> block -- is the template's
// business.
package sheet

// Row is one line of a group's table: a name and at most one image per column.
type Row struct {
	Name string
	// Cells is keyed by column name. A column with no image for this row is
	// absent from the map rather than present and empty.
	//
	// Between collection and publication the values are repository-relative
	// paths; main rewrites them to URLs once the commit holding them exists.
	Cells map[string]string
}

// Cell returns the value for a column, or "" when the row has no image there.
// Templates use this instead of `index .Cells "light"` so a missing column
// reads the same as an empty one.
func (r Row) Cell(column string) string { return r.Cells[column] }

// Group is one table. Name is the first capture of the layout expression, and
// is empty when the layout has no `group` -- a flat directory produces a single
// nameless group.
type Group struct {
	Name string
	// Columns is repeated on every group so a template that ranges over groups
	// can render a header without reaching back to the context.
	Columns []string
	Rows    []Row
}

// Total counts the images across every group.
func Total(groups []Group) int {
	n := 0
	for _, g := range groups {
		for _, r := range g.Rows {
			n += len(r.Cells)
		}
	}
	return n
}
