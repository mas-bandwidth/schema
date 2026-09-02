// The BLOCK FORM's refusals (SPEC-TABLES.md §2.7, §11, §19).
//
// `| block` declares no construct, so nothing here checks a new syntax: every
// refusal names a declaration the FORM cannot carry — a pitch that does not
// exist, an element another language cannot point at, or a field named after
// the projection's generated prologue.
package check

import (
	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockPrologueNames are the two generated halves of a block projection's
// prologue (SPEC-TABLES.md §19.1). A field of a block-form table may not be
// named after either, exactly as a field may not collide with an optional's
// `<field>_present` companion (§11).
var blockPrologueNames = []string{"magic", "layout_id"}

// checkBlockForm runs every `| block` refusal over the unit's marked tables.
// It runs AFTER checkTables, so the table closure and the derived mode are
// both settled and a diagnostic can name which of them the marker met.
func (c *checker) checkBlockForm() {
	var marked []string
	for _, name := range sortedKeys(c.tables) {
		if c.tables[name].Block {
			marked = append(marked, name)
		}
	}
	if len(marked) == 0 {
		return
	}
	variable := c.variableTables()

	for _, name := range marked {
		st := c.tables[name]
		pos := c.declPos(name)

		// `| block` on a VARIABLE-LENGTH table: a pointer anywhere in the
		// closure means no fixed pitch anywhere in it (§11).
		if variable[name] {
			c.errf(pos, "table %s | block: the block form is a form a FIXED table takes, and %s is variable-length — a pointer anywhere in its by-value closure means no fixed pitch anywhere in it; drop the marker or drop the pointer (SPEC-TABLES.md §2.7, §11)",
				name, name)
			continue
		}

		for _, f := range st.Fields {
			// a field named after the projection's generated prologue (§11)
			for _, reserved := range blockPrologueNames {
				if f.Name == reserved {
					c.errf(pos, "table %s | block: field %s collides with the projection's generated prologue — `magic` and `layout_id` are generated at the front of every block, as an optional's `_present` companion is generated beside its value; rename the field (SPEC-TABLES.md §19.1, §11)",
						name, f.Name)
				}
			}
			if f.Array != ir.ArrayCounted || f.KeyEnum != "" {
				continue // fixed and keyed arrays stay inline (§2.7, depth one)
			}
			// an out-of-line array whose ELEMENT is not a struct. Striding
			// what has no fixed pitch means nothing, which is the refusal in
			// one line (§11).
			var bad string
			switch {
			case f.Type.Pointer:
				bad = "a table pointer (*" + f.Type.Name + ")"
			case f.Type.Kind == ir.TString:
				bad = "a string"
			case f.Type.Kind == ir.TBytes:
				bad = "bytes"
			case f.Type.Kind == ir.TNamed:
				if ref, ok := f.Type.Ref.(*ir.Struct); ok && variable[ref.Name] {
					bad = "the variable-length table " + ref.Name
				}
			}
			if bad != "" {
				c.errf(pos, "table %s | block: the bounded array %s.%s has %s as its element, which has no fixed pitch — a block's out-of-line rows sit at a pitch, and striding what has no pitch means nothing (SPEC-TABLES.md §2.7, §11)",
					name, name, f.Name, bad)
			}
		}
	}

	// The block CLOSURE: the marked tables plus every record their out-of-line
	// arrays reach by value. Everything in it is laid out by the C ABI and
	// asserted on both sides (§19.3), so a construct with no blittable C#
	// spelling is refused here rather than emitted and garbled.
	for _, name := range marked {
		st := c.tables[name]
		if variable[name] {
			continue
		}
		c.checkBlockRecord(name, st, name, map[string]bool{})
	}
}

// checkBlockRecord refuses, anywhere in a block-form table's closure, a
// construct the two-language layout contract cannot express.
//
// A UNION is the one case, and it is OURS rather than the page's: §19.3 pins
// the C# side to `[StructLayout(LayoutKind.Sequential, Pack = 1, Size = N)]`
// with generated padding fields, and Sequential cannot overlay arms — an
// overlaid layout needs LayoutKind.Explicit, which §19.3 rules out by name.
// So a union in a block closure is refused with the follow-on named, never
// emitted with the arms laid end to end (which would pass every assert on the
// C++ side and garble every row on the C#).
func (c *checker) checkBlockRecord(root string, st *ir.Struct, where string, seen map[string]bool) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	pos := c.declPos(root)
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Union:
			c.errf(pos, "table %s | block: %s.%s is a union, and a block's layout contract has no blittable spelling for one — the C# side is Sequential with generated padding (SPEC-TABLES.md §19.3) and Sequential cannot overlay arms; a union in a block-form closure is a named follow-on (§15). Move the union out of the block form's closure, or drop the marker",
				root, where, f.Name)
			_ = ref
		case *ir.Struct:
			c.checkBlockRecord(root, ref, ref.Name, seen)
		}
	}
}

// variableTables is ir.VariableTables over the checker's own half-built
// members — the derivation cannot wait for assembly, because the marker's
// refusals need the mode.
func (c *checker) variableTables() map[string]bool {
	member := func(name string) *ir.Struct { return c.closureMember(name) }
	variable := map[string]bool{}
	names := sortedKeys(c.tables)
	for _, name := range sortedKeys(c.structs) {
		names = append(names, name)
	}
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			if variable[name] {
				continue
			}
			st := member(name)
			if st == nil {
				continue
			}
			for _, f := range st.Fields {
				if f.Type.Pointer {
					variable[name] = true
					break
				}
				if f.Type.Kind != ir.TNamed {
					continue
				}
				switch ref := f.Type.Ref.(type) {
				case *ir.Struct:
					if variable[ref.Name] {
						variable[name] = true
					}
				case *ir.Union:
					for _, v := range ref.Variants {
						if variable[v.Type] {
							variable[name] = true
						}
					}
				}
				if variable[name] {
					break
				}
			}
			if variable[name] {
				changed = true
			}
		}
	}
	return variable
}

func (c *checker) declPos(name string) ast.Pos {
	if d, ok := c.astDecls[name]; ok {
		return d.DeclPos()
	}
	return ast.Pos{}
}
