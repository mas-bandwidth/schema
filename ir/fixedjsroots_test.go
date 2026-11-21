package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestTableFixedJSRootsExcludesVariableTables pins docs/SPEC-TABLES.md §3.4's
// selection by the declaration on the JavaScript leg: "a variable table
// encodes as form 1, always", so a plain `table` whose closure would fit the
// fixed layout still gets no form-3 surface. JavaScript drops only the C++
// reference's leaf cap; it keeps the keyword, exactly as TableFixedEmitted
// does for every other leg.
func TestTableFixedJSRootsExcludesVariableTables(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package posu

table Pos
{
    x int32
    y int32
}

fixed table Cell
{
    x int32
    y int32
}
`)
	var tables []*ir.Struct
	for _, f := range u.Files {
		tables = append(tables, f.Tables...)
	}
	roots := ir.TableFixedJSRoots(u, tables)
	if len(roots) != 1 || roots[0].Name != "Cell" {
		var names []string
		for _, st := range roots {
			names = append(names, st.Name)
		}
		t.Fatalf("TableFixedJSRoots = %v, want [Cell] (Pos is not declared fixed)", names)
	}
	for _, st := range ir.TableFixedJSUnitRoots(u) {
		if st.Name == "Pos" {
			t.Fatal("TableFixedJSUnitRoots carries the variable table Pos")
		}
	}
	if ir.TableFixedEmitted(u, u.Tables["Pos"]) {
		t.Fatal("TableFixedEmitted says Pos carries form 3")
	}
}
