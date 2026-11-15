package goosetable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses nothing, counts nothing, and saves
// the canonical bytes the +0 record writes.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose (it runs the generated reader over the fixed-form corpus)")
	corpus := fixedCorpus(t)
	u := fixedUnit(t, "test/tables/FNZ.schema")
	ns := goose.Namespace(u)
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			data := gooseCorpusFile(t, corpus, vec.File)
			layoutBytes := int(binary.LittleEndian.Uint32(data[16:]))
			valueAt := 20 + layoutBytes + 8 + 4
			if got := binary.LittleEndian.Uint32(data[valueAt:]); got != 0x80000000 {
				t.Fatalf("the vector does not hold a compressed float's -0: %#08x", got)
			}
			main := fmt.Sprintf(`fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { print("read-failed"); return; }
    var values: %[1]s::%[2]s[1] = [%[1]s::%[2]s { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = %[1]s::%[2]sFixedLoad(values, data, rep);
    print("report ", n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", rep.oor, " ", (if rep.damaged { 1 } else { 0 }));
    if n != 1 { return; }
    print("zero ", (if values[0].Value == 0.0 { 1 } else { 0 }));
    print("bits ", schema_rt::fixed_f32_bits(values[0].Value));
    var out: u8[>..] = [];
    let w = %[1]s::%[2]sFixedSave(values[0..n], out);
    print("saved ", w);
    hex(out);
}
`, ns, vec.Root)
			out := runCustomFixedGoose(t, u, nil, main, map[string][]byte{"in.bin": data})
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) < 1 || lines[0] != "report 1 ok 0 0" {
				t.Fatalf("%s: the read of a compressed -0 must land one record, refusing and counting nothing:\n%s", vec.File, out)
			}
			if len(lines) != 5 {
				t.Fatalf("%s: an unreadable probe answer:\n%s", vec.File, out)
			}
			if lines[1] != "zero 1" {
				t.Fatalf("%s: a compressed -0 did not land the value 0:\n%s", vec.File, out)
			}
			if lines[2] != "bits 0" {
				t.Fatalf("%s: a compressed float's -0 kept its sign: %s", vec.File, lines[2])
			}
			want := append([]byte(nil), data...)
			clear(want[valueAt : valueAt+4])
			if lines[3] != fmt.Sprintf("saved %d", len(data)) || lines[4] != hex.EncodeToString(want) {
				t.Fatalf("%s: saving the landed +0 did not write the canonical bytes\n got %s %s\nwant %d %s",
					vec.File, lines[3], lines[4], len(data), hex.EncodeToString(want))
			}
		})
	}
}
