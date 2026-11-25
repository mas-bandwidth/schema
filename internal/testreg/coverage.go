package testreg

import (
	"fmt"
	"io"
	"strings"
)

// Cell states of the coverage matrix.
const (
	CellAnswered = "ok"
	CellUnrep    = "un"
	CellMissing  = "MISS"
)

// Coverage is the matrix of every test times every language: each cell is
// answered (the language's adapter runs it), unrepresentable (declared, with
// a reason) or MISSING.
type Coverage struct {
	Langs []string
	Tests []Test
	Cells [][]string // [test][lang]
	// Free are the language-free tests, each answered by the adapter its
	// row names or MISSING.
	Free      []Test
	FreeCells []string
}

// Cover computes the matrix against the adapters schema-test has.
func Cover(reg *Registry, adapters map[string]Adapter) *Coverage {
	c := &Coverage{Langs: reg.LangNames()}
	for _, t := range reg.Tests {
		if !t.PerLang() {
			cell := CellMissing
			if a, ok := adapters[t.Adapter]; ok && a.Answers(t) {
				cell = CellAnswered
			}
			c.Free = append(c.Free, t)
			c.FreeCells = append(c.FreeCells, cell)
			continue
		}
		c.Tests = append(c.Tests, t)
		row := make([]string, len(reg.Langs))
		for j, l := range reg.Langs {
			row[j] = CellMissing
			_, owed := reg.OwedBy(t.Key, l.Name)
			if _, ok := reg.Unrepresentable(t.Key, l.Name); ok {
				row[j] = CellUnrep
			} else if a, ok := adapters[l.Adapter]; ok && l.Ready() && !owed && a.Answers(t) {
				row[j] = CellAnswered
			}
		}
		c.Cells = append(c.Cells, row)
	}
	return c
}

// Missing counts the MISSING cells.
func (c *Coverage) Missing() int {
	n := 0
	for _, s := range c.FreeCells {
		if s == CellMissing {
			n++
		}
	}
	for _, row := range c.Cells {
		for _, s := range row {
			if s == CellMissing {
				n++
			}
		}
	}
	return n
}

// Print writes the matrix, then each language's counts with its MISSING
// cells grouped by type: the per-language work list.
func (c *Coverage) Print(out io.Writer) error {
	var w strings.Builder
	keyW := len("test")
	for _, t := range c.Tests {
		keyW = max(keyW, len(t.Key)+len(t.Tier)+1)
	}
	widths := make([]int, len(c.Langs))
	for j, l := range c.Langs {
		widths[j] = max(len(l), len(CellMissing))
	}
	fmt.Fprintf(&w, "%-*s", keyW, "tier.test")
	for j, l := range c.Langs {
		fmt.Fprintf(&w, " %-*s", widths[j], l)
	}
	w.WriteByte('\n')
	for i, t := range c.Tests {
		fmt.Fprintf(&w, "%-*s", keyW, t.Tier+"."+t.Key)
		for j := range c.Langs {
			fmt.Fprintf(&w, " %-*s", widths[j], c.Cells[i][j])
		}
		w.WriteByte('\n')
	}
	fmt.Fprintf(&w, "\n%d language-free tests (%s.<tier>.<key>):\n", len(c.Free), LangFree)
	for i, t := range c.Free {
		fmt.Fprintf(&w, "%-4s %s.%s.%s (adapter %s)\n", c.FreeCells[i], LangFree, t.Tier, t.Key, t.Adapter)
	}
	fmt.Fprintf(&w, "\n%d per-language tests x %d languages = %d cells, plus %d language-free: %d MISSING\n", len(c.Tests), len(c.Langs), len(c.Tests)*len(c.Langs), len(c.Free), c.Missing())
	for j, l := range c.Langs {
		counts := map[string]int{}
		var types []string
		byType := map[string]int{}
		for i, t := range c.Tests {
			s := c.Cells[i][j]
			counts[s]++
			if s == CellMissing {
				if byType[t.Type] == 0 {
					types = append(types, t.Type)
				}
				byType[t.Type]++
			}
		}
		line := fmt.Sprintf("%s: answered %d, unrepresentable %d, MISSING %d", l, counts[CellAnswered], counts[CellUnrep], counts[CellMissing])
		if len(types) > 0 {
			parts := make([]string, len(types))
			for k, ty := range types {
				parts[k] = fmt.Sprintf("%s %d", ty, byType[ty])
			}
			line += " (" + strings.Join(parts, ", ") + ")"
		}
		fmt.Fprintln(&w, line)
	}
	_, err := io.WriteString(out, w.String())
	return err
}
