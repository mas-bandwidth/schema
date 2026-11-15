package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 (docs/SPEC-TABLES.md §3.3;
// docs/rulings.json fixed-compressed-negzero): a fixed-form strict load
// stores a compressed float holding -0 as +0, refusing nothing and counting
// nothing, so read-then-save writes the +0 image. A plain float keeps its
// sign bit.

const fixedNegZeroSchema = `package fixednegzero

fixed table Row
{
    plain float32
    ratio float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
    tilt  float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
}
`

const fixedNegZeroHelpers = `
def nzhex(b:string) -> string:
    var o = ""
    for(b) c:
        o += lowercase(number_to_string(c, 16, 2))
    return o
def nzclean() -> fixednegzero.Row:
    let rec = fixednegzero.NewRow()
    rec.Plain = 1.5
    rec.Ratio = 0.5
    rec.Tilt = 2.5
    return rec
`

func fixedNegZeroRun(t *testing.T, g *loadUnit, body string, inputs map[string][]byte) []string {
	t.Helper()
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(fixedNegZeroHelpers)
	b.WriteString(body)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
}

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, fixedNegZeroSchema)
	plain := uint32(0x3FC00000) // 1.5
	ratio := uint32(0x3F000000) // 0.5
	tilt := uint32(0x40200000)  // 2.5
	minusZero := uint32(0x80000000)

	cleanOut := fixedNegZeroRun(t, g,
		"let w = fixednegzero.RowFixedSave([nzclean()])\nprint(nzhex(w))\n", nil)
	if len(cleanOut) != 1 {
		t.Fatalf("the clean probe does not save: %q", cleanOut)
	}
	clean, err := hex.DecodeString(cleanOut[0])
	if err != nil {
		t.Fatal(err)
	}

	wire := patch32(t, patch32(t, patch32(t, clean, plain, minusZero), ratio, minusZero), tilt, minusZero)
	got := fixedNegZeroRun(t, g, `
let wire = read_file("w0")
assert wire
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let back = [fixednegzero.NewRow()]
let report = schema_rt.TableReport {}
let n = fixednegzero.RowFixedLoad(back, wire, plan, report)
print(string(n) + "|" + report.reason)
print(number_to_string(schema_rt.f32bits(back[0].Ratio), 16, 8))
print(number_to_string(schema_rt.f32bits(back[0].Tilt), 16, 8))
print(number_to_string(schema_rt.f32bits(back[0].Plain), 16, 8))
print(nzhex(fixednegzero.RowFixedSave([back[0]])))
`, map[string][]byte{"w0": wire})
	if len(got) != 5 {
		t.Fatalf("the probe printed %d lines: %q", len(got), got)
	}
	if got[0] != "1|" {
		t.Fatalf("the strict fixed load: got %q, want it read", got[0])
	}
	if got[1] != "00000000" {
		t.Errorf("the compressed float32: got %q, want +0 with its sign bit clear", got[1])
	}
	if got[2] != "00000000" {
		t.Errorf("the second compressed float32: got %q, want +0 with its sign bit clear", got[2])
	}
	if got[3] != "80000000" {
		t.Errorf("the plain float32: got %q, want its -0 kept", got[3])
	}
	want := patch32(t, patch32(t, patch32(t, clean, plain, minusZero), ratio, 0), tilt, 0)
	if got[4] != hex.EncodeToString(want) {
		t.Errorf("the saved image: got %s, want the canonical %s", got[4], hex.EncodeToString(want))
	}
}
