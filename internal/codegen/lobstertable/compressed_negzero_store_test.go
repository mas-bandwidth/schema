package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COMPRESSED FLOAT HOLDING -0 IS STORED AS +0 (docs/SPEC-TABLES.md §3.3;
// the rulings file's fixed-compressed-negzero; Go's tabletext.StoredFloat):
// every writer stores the +0 bits for it, so a -0 that rides — under a
// non-zero default, as an array element, in a fixed record — never writes
// 0x80000000. A plain float's -0 is stored bit-exact.
const negZeroStoreSchema = `package negzerostore

table Probe
{
    plain float32
    r     float32 = 1.0 | min = -10.0, max = 10.0, resolution = 0.5
    list  [2]float32 | min = -10.0, max = 10.0, resolution = 0.5
}

fixed table Row
{
    lead  uint32 = 1
    value float32 = 1.0 | min = -1, max = 1, resolution = 0.01
    trail uint32 = 2
}
`

const negZeroStoreProgram = `import .load
import .schema_save
def nzhex(b:string) -> string:
    var o = ""
    for(b) c:
        o += lowercase(number_to_string(c, 16, 2))
    return o
def nzslot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    for(rec.fields) sl:
        if sl.fd.name == name:
            return sl
    assert false
    return rec.fields[0]
let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
nzslot(rec, "plain").cell.f = -0.0
nzslot(rec, "r").cell.f = -0.0
let l = nzslot(rec, "list")
while length(l.elems) < 2:
    push(l.elems, schema_rt.Value {})
l.elems[0].f = 2.5
l.elems[1].f = -0.0
let w, why = schema_rt.save_reasoned(rec)
print("variable " + why + "|" + nzhex(w))
let row = negzerostore.NewRow()
row.Value = -0.0
print("fixed " + nzhex(negzerostore.RowFixedSave([row])))
`

// TestLobsterCompressedNegZeroStoredAsPlusZero saves -0 in a compressed
// field under a non-zero default, as a compressed array element and in a
// fixed record, and -0 in a plain float: the compressed ones store +0's
// bits, the plain one keeps its sign.
func TestLobsterCompressedNegZeroStoredAsPlusZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", negZeroStoreSchema)})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(negZeroStoreProgram)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))), "\n")
	if len(out) != 2 || !strings.HasPrefix(out[0], "variable |") || !strings.HasPrefix(out[1], "fixed ") {
		t.Fatalf("the probe printed %q", out)
	}
	variable := strings.TrimPrefix(out[0], "variable |")
	// -0 is 00000080 little-endian: once for the plain float, never for a
	// compressed one
	if n := strings.Count(variable, "00000080"); n != 1 {
		t.Errorf("the variable save holds %d -0 patterns, want the plain float's alone: %s", n, variable)
	}
	if fixed := strings.TrimPrefix(out[1], "fixed "); strings.Contains(fixed, "00000080") {
		t.Errorf("the fixed save stores the compressed -0's sign: %s", fixed)
	}
}
