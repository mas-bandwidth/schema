// THE UNIT'S WIRE IDS (docs/SPEC-TABLES.md §6.6, "A RETAINED ID PAST THE
// CAPACITY COUNTS ONE retain_lost"): the retaining save numbers an id this
// build names through the ordinary id table, and only an id it CANNOT name
// takes an entry of the caller's bounded retained-id list. Which ids a build
// names is a unit fact, ir.TableWireIds, the same list the Go writer's
// tableRetainKnown holds; every table module registers it with the runtime so
// schema_retain.luau can tell the two apart.
package luautable

import (
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("retainids", emitRetainIds)
}

func idList(ids []uint64) string {
	var b strings.Builder
	for i, id := range ids {
		if i%4 == 0 {
			b.WriteString("\n\t")
		} else {
			b.WriteString(" ")
		}
		b.WriteString(luau.IntegerLit(new(big.Int).SetUint64(id)))
		b.WriteString(",")
	}
	return b.String()
}

// emitRetainIds writes the unit's wire ids into the module's registration
// with schema_tables (S.wireIds), in ir.TableWireIds order, and the table node
// type ids a pointer slot may place (S.tableIds) so a declared type the root
// cannot place is still told apart from an unnameable one
// (docs/SPEC-TABLES.md §3.1).
func emitRetainIds(g *luau.Gen) error {
	ids := ir.TableWireIds(g.Unit)
	tableIDs := make([]uint64, 0, len(g.Unit.Tables))
	for _, t := range g.Unit.Tables {
		tableIDs = append(tableIDs, ir.TableWireId(t.WireName()))
	}
	sort.Slice(tableIDs, func(i, j int) bool { return tableIDs[i] < tableIDs[j] })
	st := g.Require(RuntimeModule)
	g.Pf("-- the unit's wire ids, the ids this build names (docs/SPEC-TABLES.md §6.6)\n")
	g.Pf("%s.wireIds({%s\n})\n\n", st, idList(ids))
	g.Pf("-- the table type ids this reader declares, whatever a root places (§3.1)\n")
	g.Pf("%s.tableIds({%s\n})\n\n", st, idList(tableIDs))
	return nil
}
