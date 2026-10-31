package tablewire

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// idTableFile is the ten-byte empty table with n distinct raw entries folded
// in: form byte, the root's terminator, the entries, the count
// (docs/SPEC-TABLES.md §3).
func idTableFile(n int) []byte {
	out := []byte{ir.TableWireForm, 0}
	for i := 1; i <= n; i++ {
		out = binary.LittleEndian.AppendUint64(out, uint64(i))
	}
	return binary.LittleEndian.AppendUint64(out, uint64(n))
}

// THE ENTRY COUNT'S MAXIMUM (docs/SPEC-TABLES.md §3, THE DISTINCTNESS CHECK;
// docs/LAYERS/1-framing.md F6): a table of exactly the bound resolves, and one
// entry more is malformed on the whole-wire rule, whatever the buffer holds.
func TestIdTableAtTheBoundResolvesAndOneOverIsMalformed(t *testing.T) {
	at := idTableFile(ir.TableWireIdTableBound)
	if _, ids, ok := trailer(at); !ok || len(ids) != ir.TableWireIdTableBound {
		t.Fatalf("a table of %d entries is the bound and must resolve", ir.TableWireIdTableBound)
	}
	over := idTableFile(ir.TableWireIdTableBound + 1)
	if _, _, ok := trailer(over); ok {
		t.Fatalf("a table of %d entries is above the bound and must be malformed", ir.TableWireIdTableBound+1)
	}
	// THE NEGATIVE CONTROL: the same bytes under a bound one wider resolve,
	// so it is the bound that refuses them and not the buffer or the entries.
	if _, ids, ok := trailerBounded(over, ir.TableWireIdTableBound+1); !ok || len(ids) != ir.TableWireIdTableBound+1 {
		t.Fatal("the reversed bound still refuses, so the check above is not the bound's")
	}
}
