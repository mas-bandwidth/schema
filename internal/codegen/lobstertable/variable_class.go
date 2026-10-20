// The variable class's EMITTED census: pointers, arrays of pointers, unions
// with table arms, union arms of any field type, payload-free arms and arrays
// of unions (docs/SPEC-TABLES.md §2.1, §2.6, §2.9, §3.1; docs/PORT-LOBSTER.md,
// THE LOAD). The wire itself is the runtime walk (runtime/load.lobster and
// runtime/schema_save.lobster, joined by runtime/variable_class.lobster):
// this file joins every table module through [Register] with the per-module
// census the card's test holds the generated surface to, so a descriptor and
// the runtime cannot disagree about which of these constructs a module uses.
package lobstertable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("variable-class", emitVariableClass) }

// VariableClassCounts is one file's census of the variable-class constructs
// (docs/SPEC-TABLES.md §2.1, §2.6, §2.9). Pointers counts a *T field, never a
// *string or *bytes (those are blobs, §2.5); PointerArrays counts a [N]*T,
// [..N]*T or []*T field (an array of pointers is a pointer slot per element,
// §2.1); UnionsWithTableArms counts a union declaring a by-value table arm;
// UnionArms counts every arm, payload-free included, because an arm is a field
// line and any field type is legal (§2.6); PayloadFreeArms counts the bare
// name; UnionArrays counts an array whose element is a union.
type VariableClassCounts struct {
	Pointers            int
	PointerArrays       int
	UnionsWithTableArms int
	UnionArms           int
	PayloadFreeArms     int
	UnionArrays         int
}

// Total is every construct this file's declarations use.
func (c VariableClassCounts) Total() int {
	return c.Pointers + c.PointerArrays + c.UnionsWithTableArms + c.UnionArms + c.PayloadFreeArms + c.UnionArrays
}

// VariableClassCensus counts one file's declarations: every table, type and
// table-closure union it declares (docs/SPEC-TABLES.md §2). A by-value field's
// own declaration is counted where that declaration lives, so a nested table
// is never counted twice.
func VariableClassCensus(f *ir.File) VariableClassCounts {
	var c VariableClassCounts
	seenStruct := map[string]bool{}
	seenUnion := map[string]bool{}
	countStruct := func(st *ir.Struct) {
		if st == nil || seenStruct[st.Name] {
			return
		}
		seenStruct[st.Name] = true
		for _, fld := range st.Fields {
			if fld.Array != ir.ArrayNone {
				if fld.Type.Pointer && !fld.Type.Blob() {
					c.PointerArrays++
				}
				if un, ok := fld.Type.Ref.(*ir.Union); ok && fld.Type.Kind == ir.TNamed && un != nil {
					c.UnionArrays++
				}
				continue
			}
			if fld.Type.Pointer && !fld.Type.Blob() {
				c.Pointers++
			}
		}
	}
	countUnion := func(un *ir.Union) {
		if un == nil || seenUnion[un.Name] {
			return
		}
		seenUnion[un.Name] = true
		tableArm := false
		for _, v := range un.Variants {
			if v.Void() {
				c.PayloadFreeArms++
				c.UnionArms++
				continue
			}
			c.UnionArms++
			if v.Body() {
				tableArm = true
			}
		}
		if tableArm {
			c.UnionsWithTableArms++
		}
	}
	// a table is kept beside Decls, not inside it (ir.File), and a
	// table-closure union beside it in TableUnions, so both kinds are
	// counted from their own lists and the Decls walk catches packet types.
	for _, st := range f.Tables {
		countStruct(st)
	}
	for _, un := range f.TableUnions {
		countUnion(un)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			countStruct(d)
		case *ir.Union:
			countUnion(d)
		}
	}
	return c
}

// emitVariableClass joins one table module with its census. The string is the
// six counts in [VariableClassCounts]' field order, so the card's test can read
// what the emitter saw out of the generated module and hold it to the IR.
func emitVariableClass(g *lobster.Gen) error {
	c := VariableClassCensus(g.File)
	g.Pf("// %sVariableClassCensus is the variable-class census of %s's declarations:\n", g.File.Base, g.File.Base)
	g.Pf("// pointers, arrays of pointers, unions with table arms, union arms, payload-free\n")
	g.Pf("// arms and arrays of unions (docs/SPEC-TABLES.md §2.1, §2.6, §2.9, §3.1).\n")
	g.Pf("def %sVariableClassCensus() -> string:\n", g.File.Base)
	g.Pf("    return %s\n\n", lobster.StringLit([]byte(fmt.Sprintf("%d,%d,%d,%d,%d,%d",
		c.Pointers, c.PointerArrays, c.UnionsWithTableArms, c.UnionArms, c.PayloadFreeArms, c.UnionArrays))))
	return nil
}
