package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A MAP COUNT WHOSE LEB RUNS PAST THE MAP'S OWN L IS DAMAGE, IN EVERY
// DETECTION ORDER (docs/SPEC-TABLES.md §3, §6.5; tla/ReadOrder.tla,
// OneVerdict). The array and keyed headers refuse a count that crosses their L;
// the map header lacked the guard, so the decode-first order (wireLast) sliced
// past L and panicked on bytes the wire check refuses. The mutant: the map
// field `named` with L = 2 and body `0D 80`, whose count LEB borrows the next
// field's reference byte (`80 02` = 256) and ends one byte past L.
func TestMapCountCrossingItsLRefusesInEveryOrder(t *testing.T) {
	t.Parallel()
	m := covModel(t, oneVerdictUnit)
	root := m.Lookup("Root")
	body := []byte{
		1, ir.TableKindArray, 2, ir.TableKindTable, 0x80, // named: L 2, element kind 13, a count LEB cut at L
		2, ir.TableKindBool, 1, // flag = true: its reference byte ends the count LEB
		0, // the root's terminator
	}
	data := append([]byte{ir.TableWireForm}, body...)
	data = appendU64(data, ir.TableWireId("named"))
	data = appendU64(data, ir.TableWireId("flag"))
	data = appendU64(data, 2)
	for i, order := range []readOrder{{}, {wireLast: true}} {
		if got := oneVerdictRead(t, m, root, data, order); got.accepted {
			t.Fatalf("order %d accepted a map count that crosses its L: %x", i, data)
		}
	}
}
