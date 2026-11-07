package mootable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6), in moo. The walks are the
// runtime's (runtime/table_retain.moo, with hooks in table_load.moo,
// table_save.moo and table_message.moo that a plain read and save never
// reach). What the compiler owes it is the ONE fact a retaining save cannot
// read off a descriptor: which ids THIS UNIT CAN SPELL (ir.TableWireIds),
// because an id a retained record names takes its id-table entry from the
// generated table when this build can name it and from the caller's own list
// when it cannot. Every module registers the unit's whole set, so whichever
// table module a program requires, the set is there.
func init() {
	Register("retain", emitRetain)
}

func emitRetain(g *Gen) error {
	ids := ir.TableWireIds(g.Unit)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = IdLit(id)
	}
	g.Pf("-- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the ids this unit's closure can\n")
	g.Pf("-- spell, so a retaining save knows which retained ids take the caller's list\n")
	g.Pf("St.known({ %s })\n\n", strings.Join(parts, ", "))
	return nil
}
