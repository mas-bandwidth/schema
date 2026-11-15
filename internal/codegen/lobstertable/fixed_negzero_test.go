package lobstertable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 (docs/SPEC-TABLES.md §3.3;
// docs/rulings.json fixed-compressed-negzero). The vector is the reference
// writer's file holding the sign bit alone in the compressed field; this leg
// reads it through its generated strict fixed load, lands the value 0 with
// the sign bit clear, refuses nothing and counts nothing, and saves the
// canonical bytes the +0 record writes.

const fixedNegZeroProbe = `
def nzhex(b:string) -> string:
    var o = ""
    for(b) c:
        o += lowercase(number_to_string(c, 16, 2))
    return o
let wire = read_file("in.bin")
assert wire
let plan:[schema_rt.TableFixedEntry] = []
for(64): push(plan, schema_rt.TableFixedEntry {})
let back = [tblfnz.NewNegZero()]
let report = schema_rt.TableReport {}
let n = tblfnz.NegZeroFixedLoad(back, wire, plan, report)
print(string(n))
print("verdict=" + string(report.verdict) + " fixed_raised=" + string(report.fixed_raised) + " reason=" + report.reason + " layout_hash=" + string(report.layout_hash) + " widened=" + string(report.widened) + " unknown=" + string(report.unknown) + " kind_mismatch=" + string(report.kind_mismatch) + " clamped=" + string(report.clamped) + " duplicate=" + string(report.duplicate) + " retained=" + string(report.retained) + " retain_lost=" + string(report.retain_lost) + " malformed=" + string(report.malformed) + " non_finite_float=" + report.non_finite_float)
print(number_to_string(schema_rt.f32bits(back[0].Value), 16, 8))
print(nzhex(tblfnz.NegZeroFixedSave(back)))
`

// fixedNegZeroClean is the report line of a load that refused nothing and
// counted nothing: every TableReport field at its zero value.
const fixedNegZeroClean = "verdict=0 fixed_raised=false reason= layout_hash=0 widened=0 unknown=0 kind_mismatch=0 clamped=0 duplicate=0 retained=0 retain_lost=0 malformed=false non_finite_float="

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpusDir(t)
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatalf("%s: %v", vec.File, err)
			}
			const header = 16 // TableFixedHeaderBytes
			layoutBytes := int(binary.LittleEndian.Uint32(data[header:]))
			valueAt := header + 4 + layoutBytes + 8 + 4
			if got := binary.LittleEndian.Uint32(data[valueAt:]); got != 0x80000000 {
				t.Fatalf("the vector does not hold a compressed float's -0: %#08x", got)
			}
			g, err := generateLoadUnit(t, []string{filepath.Join(root, "test", "tables", "FNZ.schema")})
			if err != nil {
				t.Fatalf("FNZ.schema: %v", err)
			}
			if vec.Root != "NegZero" {
				t.Fatalf("the probe reads root NegZero, the vector names %s", vec.Root)
			}
			var b strings.Builder
			for _, m := range g.mods {
				fmt.Fprintf(&b, "import .%s\n", m)
			}
			b.WriteString(fixedNegZeroProbe)
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"in.bin": data})), "\n")
			got := strings.Split(out, "\n")
			if len(got) != 4 {
				t.Fatalf("the probe printed %d lines: %q", len(got), got)
			}
			if got[0] != "1" {
				t.Fatalf("the reader refused a compressed -0: n=%s %s", got[0], got[1])
			}
			if got[1] != fixedNegZeroClean {
				t.Errorf("the read of a compressed -0 reported %s, want %s", got[1], fixedNegZeroClean)
			}
			if got[2] != "00000000" {
				t.Errorf("a compressed float's -0 kept its sign: %s, want +0 with its sign bit clear", got[2])
			}
			want := append([]byte(nil), data...)
			clear(want[valueAt : valueAt+4])
			if got[3] != hex.EncodeToString(want) {
				t.Errorf("saving the landed +0 did not write the canonical bytes:\n got %s\nwant %s", got[3], hex.EncodeToString(want))
			}
		})
	}
}
