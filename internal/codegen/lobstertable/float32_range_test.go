package lobstertable_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC.md §4.6;
// docs/SPEC-TABLES.md §3.3; Go's tabletext.FloatInRange): max = 0.1 and
// min = 0.7 are ends float32 cannot spell, so the float32 nearest each is in
// range and one float32 step past it is not, on every write and every read.
// A writer that admits a value its own reader then refuses is the defect
// this holds shut (LOBSTER-1).
const float32RangeSchema = `package f32range

table Probe
{
    hi float32 = 0.05 | min = 0.0, max = 0.1, resolution = 0.05
    lo float32 = 0.8 | min = 0.7, max = 1.0, resolution = 0.1
}
`

const float32RangeBody = `
def f32of(bits:int) -> float:
    return read_float32_le(write_int32_le(schema_rt.zeros(4), 0, bits), 0)

def probe(hi:int, lo:int):
    let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
    fslot(rec, "hi").cell.f = f32of(hi)
    fslot(rec, "lo").cell.f = f32of(lo)
    let w, why = schema_rt.save_reasoned(rec)
    if why != "":
        print("save refused")
    else:
        let back, reason = schema_rt.load_strict("Probe", w, schema_rt.LoadReport {})
        print("save " + fhex(w) + " load " + (if back: "ok" else: "refused " + reason))
    let js, jwhy = schema_rt.json_write(rec)
    if jwhy != "":
        print("json refused")
    else:
        let jback = schema_rt.json_read("Probe", js, schema_rt.LoadReport {})
        print("json read " + (if jback: "ok" else: "refused"))

// the float32 nearest each end, then one float32 step past it
probe(0x3DCCCCCD, 0x3F333333)
probe(0x3DCCCCCE, 0x3F333333)
probe(0x3DCCCCCD, 0x3F333332)
`

func TestLobsterFloat32RangeHeldAtFloat32(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g := finiteGenerate(t, float32RangeSchema)
	out := finiteRun(t, g, "import .json\n"+float32RangeBody, nil)
	if len(out) != 6 {
		t.Fatalf("the probe printed %q", out)
	}
	// the ends: the save writes them, and the variable read and the JSON
	// read take back what the writers wrote
	save, rest, ok := strings.Cut(strings.TrimPrefix(out[0], "save "), " load ")
	if !strings.HasPrefix(out[0], "save ") || !ok || rest != "ok" {
		t.Errorf("the float32 at each end: %q, want the variable save read back", out[0])
	}
	if out[1] != "json read ok" {
		t.Errorf("the float32 at each end: %q, want the JSON writer's text read back", out[1])
	}
	// the same bytes with each end's value one float32 step outside: the
	// variable read refuses them
	wire, err := hex.DecodeString(save)
	if err != nil || !ok || len(wire) == 0 {
		t.Fatalf("the float32 at each end: the save printed %q (%v), not the wire to step past each end of", out[0], err)
	}
	{
		past := map[string][]byte{
			"hi": patch32(t, wire, 0x3DCCCCCD, 0x3DCCCCCE),
			"lo": patch32(t, wire, 0x3F333333, 0x3F333332),
		}
		got := finiteRun(t, g, `
for(["hi", "lo"]) f:
    let data = read_file(f)
    assert data
    let back, reason = schema_rt.load_strict("Probe", data, schema_rt.LoadReport {})
    print(f + " " + (if back: "ok" else: reason))
`, past)
		if strings.Join(got, ";") != "hi out_of_range;lo out_of_range" {
			t.Errorf("one step past each end on the read: %q, want both refused out_of_range", got)
		}
	}
	// one float32 step past either end: every writer refuses
	for i, row := range [][]string{out[2:4], out[4:6]} {
		if row[0] != "save refused" || row[1] != "json refused" {
			t.Errorf("one step past end %d: %q, want both writers to refuse", i, row)
		}
	}
}
