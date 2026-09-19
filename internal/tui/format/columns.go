package format

// Col describes one column of a terminal table to Fit.
//
// Width is what the column wants in display cells. Min is the floor it may
// be squeezed to before it is dropped instead; a Min of 0 means the column
// is fixed and never shrinks. Prio is the drop order — the lowest-Prio
// column goes first — and Flex marks a column that absorbs whatever cells
// are left once every other column is placed.
type Col struct {
	Key   string
	Width int
	Min   int
	Prio  int
	Flex  bool
}

// Layout is the set of columns that survived a Fit, in table order, with
// Width set to the cells each one actually gets.
type Layout []Col

// Has reports whether the column survived the fit.
func (l Layout) Has(key string) bool {
	for _, c := range l {
		if c.Key == key {
			return true
		}
	}
	return false
}

// Cells returns the column's width, or 0 when it was dropped. A caller can
// switch on Has and size with Cells without holding an index.
func (l Layout) Cells(key string) int {
	for _, c := range l {
		if c.Key == key {
			return c.Width
		}
	}
	return 0
}

// Total returns the cells the layout occupies including the gutter and one
// separator between adjacent columns — the number Fit guarantees is within
// the width it was given.
func (l Layout) Total(gutter int) int {
	if len(l) == 0 {
		return 0
	}
	n := gutter + len(l) - 1
	for _, c := range l {
		n += c.Width
	}
	return n
}

// Fit decides which columns survive a frame of the given width and how many
// cells each one gets. gutter is the fixed lead the caller spends before the
// first column — the cursor column in every table here — and one cell is
// charged between adjacent columns.
//
// Columns are shed lowest Prio first, and a column with a Min is squeezed
// toward it before any column is dropped, so a frame loses detail gradually
// rather than losing a whole column while another still has slack. Fit
// returns nil when not even the highest-Prio column fits at its Min, which
// is the caller's signal to print its own "too narrow" line.
//
// The guarantee is Total(gutter) <= width. It is load-bearing rather than
// cosmetic: the content panel word-wraps a line it cannot fit
// (lipgloss Style.Width), and a row that wraps to two screen lines pushes
// the last row out of the panel and desynchronizes the click hit-test,
// which maps a mouse row straight to a data-row index.
//
// Widths are display cells, not runes. A caller padding with fmt must route
// variable-length content through Truncate first, since fmt's width verbs
// count runes and a double-width glyph then spends two cells for one.
func Fit(cols []Col, width, gutter int) Layout {
	if width <= 0 || gutter < 0 || len(cols) == 0 {
		return nil
	}

	// Work on a copy: Fit is called every frame with a caller-owned slice.
	live := make(Layout, 0, len(cols))
	for _, c := range cols {
		if c.Width < 0 {
			c.Width = 0
		}
		if c.Min < 0 {
			c.Min = 0
		}
		if c.Min > c.Width {
			c.Min = c.Width
		}
		live = append(live, c)
	}

	for len(live) > 0 {
		if fitted, ok := squeeze(live, width, gutter); ok {
			return fitted
		}
		live = dropLowest(live)
	}
	return nil
}

// squeeze tries to place every column in live within width, shrinking
// shrinkable columns toward their Min lowest-Prio first, and handing any
// leftover cells to the Flex columns. It reports whether the set fits.
func squeeze(live Layout, width, gutter int) (Layout, bool) {
	out := make(Layout, len(live))
	copy(out, live)

	over := out.Total(gutter) - width
	if over > 0 {
		// Shrink the least valuable columns first, each down to its Min.
		for _, i := range byPrioAsc(out) {
			if over <= 0 {
				break
			}
			// Min 0 marks a fixed column. A glyph bar or a sparkline at
			// half width is not a smaller version of itself, so those are
			// dropped whole rather than squeezed.
			if out[i].Min <= 0 {
				continue
			}
			slack := out[i].Width - out[i].Min
			if slack <= 0 {
				continue
			}
			take := min(slack, over)
			out[i].Width -= take
			over -= take
		}
	}
	if out.Total(gutter) > width {
		return nil, false
	}

	// Hand what is left to the flexible columns, lowest index first so the
	// remainder lands in a stable place across frames.
	if spare := width - out.Total(gutter); spare > 0 {
		var flex []int
		for i := range out {
			if out[i].Flex {
				flex = append(flex, i)
			}
		}
		if len(flex) > 0 {
			share, extra := spare/len(flex), spare%len(flex)
			for n, i := range flex {
				out[i].Width += share
				if n < extra {
					out[i].Width++
				}
			}
		}
	}
	return out, true
}

// byPrioAsc returns indices into live ordered by ascending Prio, so the
// column the caller values least is squeezed and dropped first. Ties break
// on position, keeping the result stable frame to frame.
func byPrioAsc(live Layout) []int {
	idx := make([]int, len(live))
	for i := range idx {
		idx[i] = i
	}
	// Insertion sort: a table has a handful of columns, and this keeps the
	// ordering obviously stable without pulling in a comparator.
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && live[idx[j]].Prio < live[idx[j-1]].Prio; j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	return idx
}

// dropLowest removes the single lowest-Prio column, breaking ties on the
// later position so a table sheds from the right.
func dropLowest(live Layout) Layout {
	worst := 0
	for i := 1; i < len(live); i++ {
		if live[i].Prio <= live[worst].Prio {
			worst = i
		}
	}
	return append(live[:worst:worst], live[worst+1:]...)
}
