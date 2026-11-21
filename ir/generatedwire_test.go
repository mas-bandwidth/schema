package ir_test

// A GENERATED record's identity in the two projections (F-004, F-005): a map's
// entry and a text list's element record are generated under names derived
// from the holder's and the field's SOURCE spellings, and no byte carries
// either. A `was` rename of the holder table or of the field keeps every wire
// id, so it must move no line of the cook projection and not the build
// version — the record keys, the entry order and the `type=` a holder's text
// list names included. The controls are the same edits made BARE, which move
// the wire ids and so must move the projection.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestAWasRenameOfAGeneratedRecordsHolderMovesNothing(t *testing.T) {
	const holder = "package referents\n\ntable %s\n{\n    %s\n}\n"
	src := func(table, field string) string {
		return strings.Replace(strings.Replace(holder, "%s", table, 1), "%s", field, 1)
	}
	for _, decl := range []string{
		"map[uint16]int32",
		"map[uint16]map[uint16]int32",
		"[]string(3)",
		"[..4]string(3)",
		"[]?string(3)",
	} {
		t.Run(decl, func(t *testing.T) {
			before := unitFrom(t, src("Root", "cells "+decl))
			for _, edit := range []struct {
				name, table, field string
				moves              bool
			}{
				{"the holder renamed under was", `Record | was = "Root"`, "cells " + decl, false},
				{"the field renamed under was", "Root", "labels " + decl + ` | was = "cells"`, false},
				{"both renamed under was", `Record | was = "Root"`, "labels " + decl + ` | was = "cells"`, false},
				{"the holder renamed bare (control)", "Record", "cells " + decl, true},
				{"the field renamed bare (control)", "Root", "labels " + decl, true},
			} {
				after := unitFrom(t, src(edit.table, edit.field))
				a, b := ir.CookProjection(before), ir.CookProjection(after)
				if moved := a != b; moved != edit.moves {
					t.Errorf("%s: projection moved=%v, want %v\n--- before ---\n%s\n--- after ---\n%s", edit.name, moved, edit.moves, a, b)
				}
				if moved := ir.BuildVersion(before) != ir.BuildVersion(after); moved != edit.moves {
					t.Errorf("%s: build version moved=%v, want %v", edit.name, moved, edit.moves)
				}
			}
		})
	}
}

// TestAnUnrenamedGeneratedRecordKeepsItsName: with nothing renamed the stable
// spelling IS the generated name, so a unit that has not moved projects
// exactly as it did before the rule — no existing cook is invalidated by it.
func TestAnUnrenamedGeneratedRecordKeepsItsName(t *testing.T) {
	u := unitFrom(t, "package referents\n\ntable Root\n{\n    cells []string(3)\n    tags  [..4]?string(3)\n}\n")
	root := u.Tables["Root"]
	for _, f := range root.Fields {
		st := f.TextListElem()
		if st == nil {
			t.Fatalf("%s holds no text list", f.Name)
		}
		if got := ir.GeneratedWireName(u, st); got != st.Name {
			t.Errorf("%s: GeneratedWireName = %q, the generated name is %q", f.Name, got, st.Name)
		}
	}
}
