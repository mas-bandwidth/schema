// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6; docs/PORT-LOBSTER.md, THE LOAD and
// THE RUNTIME LAYOUT). The wire is the runtime walk (runtime/load.lobster's
// RetainCapture hook, runtime/schema_save.lobster's RetainTail hook, and
// runtime/retain_unknown.lobster joining them): LoadRetain keeps the unknown
// ids and SaveRetain writes them back at the end of the body that carried
// them. This file joins the descriptor emitter through [Register] with the
// per-module census the card's test names, so a descriptor and the runtime
// cannot disagree about which path-step constructs a module can carry.
package lobstertable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("retain-unknown", emitRetainUnknown) }

// RetainUnknownCounts is one file's census of the constructs a retained
// record's PATH can descend through (docs/SPEC-TABLES.md §6.6, THE PATH):
// nested tables, arrays, enum-keyed arrays, unions, maps, unbounded lists and
// pointers. Each is a body or an element a retained record is addressed under,
// which is what makes the step pair the walk uses.
type RetainUnknownCounts struct {
	NestedTables int
	Arrays       int
	KeyedArrays  int
	Unions       int
	Maps         int
	Lists        int
	Pointers     int
}

// Total is every addressable construct this file's declarations use.
func (c RetainUnknownCounts) Total() int {
	return c.NestedTables + c.Arrays + c.KeyedArrays + c.Unions + c.Maps + c.Lists + c.Pointers
}

// RetainUnknownCensus counts one file's declarations. A by-value field's own
// declaration is counted where that declaration lives, so a nested table is
// never counted twice.
func RetainUnknownCensus(f *ir.File) RetainUnknownCounts {
	var c RetainUnknownCounts
	note := func(fld *ir.Field) {
		if fld.Array != ir.ArrayNone {
			c.Arrays++
		}
		if fld.KeyEnum != "" {
			c.KeyedArrays++
		}
		if fld.IsMap() {
			c.Maps++
		}
		if fld.IsList() {
			c.Lists++
		}
		if fld.Type.Pointer && !fld.Type.Blob() {
			c.Pointers++
		}
		if fld.Type.Kind == ir.TNamed {
			switch ref := fld.Type.Ref.(type) {
			case *ir.Union:
				if ref != nil && !fld.Type.Pointer {
					c.Unions++
				}
			case *ir.Struct:
				if ref != nil && !fld.Type.Pointer && fld.Array == ir.ArrayNone && !fld.IsMap() {
					c.NestedTables++
				}
			}
		}
	}
	seenStruct := map[string]bool{}
	seenUnion := map[string]bool{}
	visitStruct := func(st *ir.Struct) {
		if st == nil || seenStruct[st.Name] {
			return
		}
		seenStruct[st.Name] = true
		for _, fld := range st.Fields {
			note(fld)
		}
	}
	visitUnion := func(un *ir.Union) {
		if un == nil || seenUnion[un.Name] {
			return
		}
		seenUnion[un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				note(v.F)
			}
		}
	}
	for _, st := range f.Tables {
		visitStruct(st)
	}
	for _, un := range f.TableUnions {
		visitUnion(un)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			visitStruct(d)
		case *ir.Union:
			visitUnion(d)
		}
	}
	return c
}

// emitRetainUnknown joins one table module with its retention census. The
// string is the seven counts in [RetainUnknownCounts]' field order, so the
// card's test can read what the emitter saw out of the generated module and
// hold it to the IR.
func emitRetainUnknown(g *lobster.Gen) error {
	c := RetainUnknownCensus(g.File)
	g.Pf("// %sRetainUnknownCensus is the retain-unknown census of %s's declarations:\n", g.File.Base, g.File.Base)
	g.Pf("// nested tables, arrays, enum-keyed arrays, unions, maps, lists and pointers,\n")
	g.Pf("// the path-step constructs of docs/SPEC-TABLES.md §6.6.\n")
	g.Pf("def %sRetainUnknownCensus() -> string:\n", g.File.Base)
	g.Pf("    return %s\n\n", lobster.StringLit([]byte(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d",
		c.NestedTables, c.Arrays, c.KeyedArrays, c.Unions, c.Maps, c.Lists, c.Pointers))))
	return nil
}
