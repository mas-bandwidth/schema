package squirreltable_test

// THE WIRE-ALONE CHECK (docs/SPEC-TABLES.md §6.5; wireCheck in
// schema_table_load.nut, a port of internal/tablewire/wirecheck.go): every
// byte of a form-1 file is held to the wire's own rules, the bytes the walk
// skips or never reaches included. The release wire fuzz found the Squirrel
// load accepting a graphdemo.Scene mutant whose ill-formed text sat where the
// walk never validated it, where the engine refuses wire_damaged.

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const (
	// sqWireCheckCall is the load's one call of the check, and
	// sqWireCheckOff the state a check that saw nothing answers.
	sqWireCheckCall = "wireCheck(bytes, 1, end, table, false)"
	sqWireCheckOff  = "{ fail = null, nonFinite = false }"
)

// withoutWireCheck answers the load runtime with its wire-alone check turned
// off, for a negative control whose rule the check also holds.
func withoutWireCheck(t *testing.T, src string) string {
	t.Helper()
	if n := strings.Count(src, sqWireCheckCall); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the wire check's call appears %d times in schema_table_load.nut, want once", n)
	}
	return strings.Replace(src, sqWireCheckCall, sqWireCheckOff, 1)
}

// graphdemo.Scene with a zero byte inside a string the walk does not read
// (fuzz mutant, release wire fuzz).
const sqUnreachedTextMutant = "01010c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c050069676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfc7f39b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"

var sqUnreachedTextCase = sqVectorCase{"unreached_text", "graphdemo", "tables/pointers", "Scene",
	"schema_table_load.nut", sqWireCheckCall, sqWireCheckOff}

func TestSquirrelWireCheckHoldsUnreachedBytes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	wire, err := hex.DecodeString(sqUnreachedTextMutant)
	if err != nil {
		t.Fatal(err)
	}
	if v := sqVectorVerdictOf(t, sqUnreachedTextCase, false, wire); v != "" {
		t.Fatalf("graphdemo.Scene: %s", v)
	}
}

// TestSquirrelWireCheckNegativeControl turns the check off: the walk alone
// accepts the mutant the engine refuses, and the verdict must go red.
func TestSquirrelWireCheckNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	wire, err := hex.DecodeString(sqUnreachedTextMutant)
	if err != nil {
		t.Fatal(err)
	}
	if v := sqVectorVerdictOf(t, sqUnreachedTextCase, true, wire); v == "" {
		t.Fatal("NEGATIVE CONTROL FAILED: with the wire check off, the unreached ill-formed text left the verdict green")
	}
}
