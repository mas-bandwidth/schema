package check

import (
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// checkFixedTableClosures is the `fixed table` refusal (docs/SPEC-TABLES.md
// §2.2, §11), and the whole reason the keyword exists — the owner's ruling:
// "maybe we should be explicit with `fixed` to tables in schema lang… that way
// if we add any feature that stops it from being fixed, it is a compile error".
//
// A `fixed table` declares a body of one size. Six constructs in its BY-VALUE
// closure make a body variable size, and each is refused HERE, naming the
// field and the fixed table it breaks, rather than silently moving the table
// to the other wire: a map (§2.8), an unbounded array (§2.9), a pointer
// (§2.1), a byte buffer at its used size (§2.5), a guarded (`if`) field
// (SPEC §4.5), and a nested plain `table`.
//
// It also settles the class of the tables NOBODY DECLARES — the generated
// `{ key, value }` entry of a map (§2.8). No source line carries a keyword for
// one, so the entry takes the class its own body can hold, which is the same
// walk read as an answer instead of as a refusal.
func (c *checker) checkFixedTableClosures(names []string) {
	// SORTED and over the closure, so the diagnostics do not shuffle run to
	// run and a table is judged after every member it reaches is resolved.
	entries := make([]string, 0, len(names))
	for _, name := range names {
		st := c.tables[name]
		if st == nil {
			continue
		}
		if st.MapEntryOf != "" {
			entries = append(entries, name)
			continue
		}
		if !st.FixedDeclared {
			continue
		}
		pos := ast.Pos{}
		if d, ok := c.astDecls[name]; ok {
			pos = d.DeclPos()
		}
		for _, b := range ir.FixedClosureBreaks(c.closureMember, name) {
			c.errf(pos, "fixed table %s: %s %s; drop `fixed` from %s, or take the construct out of its closure (%s)",
				name, b.At(), b.Why, name, b.Refs)
		}
	}
	// The entries settle last: an entry's value may name any closure member,
	// and every declared class is known by now.
	sort.Strings(entries)
	for _, name := range entries {
		c.tables[name].FixedDeclared = len(ir.FixedClosureBreaks(c.closureMember, name)) == 0
	}
}
