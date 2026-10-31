package luautable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuauCookOrderWordNegativeControl is I7's control (docs/PORTING.md,
// cross-endian refusal as a named gate). It removes the cook check's order
// word test from a scratch copy of the emitted runtime and requires the
// order_word case of TestLuauCookOpenAgreesWithGo (the magic intact, the
// order word saying the other order) to stop answering Go's verdict, while
// the clean cook still opens: the gate sees the order word, the half no
// harness surface forges.
func TestLuauCookOrderWordNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "\tif buffer.readinteger(bytes, at + 16) ~= 1i then return \"not_a_cook\" end\n"
	units, _ := readManifest(t)
	cu := buildCookUnit(t, units, "tabledemo")
	src, ok := cu.g.files["schema_cookread.luau"]
	if !ok {
		t.Fatal("the unit emits no schema_cookread.luau")
	}
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the order-word anchor %q is not in the runtime exactly once", anchor)
	}
	table := cu.m.Lookup("RootConfig")
	if table == nil {
		t.Fatal("tabledemo has no RootConfig")
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "root_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inst := cu.m.New(table)
	var report tabletext.Report
	if _, err := tablewire.Decode(cu.m, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	cooked, err := tablecook.Cook(cu.m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	layout := ir.RecordLayout(cu.u, table)
	build := fmt.Sprintf("0x%016x", cu.build)
	forged := mutate(cooked, func(b []byte) { binary.LittleEndian.PutUint64(b[16:], 2) })
	want := goCookReason(forged, cu.build, layout.Size, layout.Align)
	if want != "not_a_cook" {
		t.Fatalf("Go answers %s for the forged order word, not not_a_cook", want)
	}
	probe := cookProbe(cu.g, "RootConfig", build, false)
	if got := runProbe(t, cu.g, probe, map[string][]byte{"wire": wire, "cook": forged}); got != want {
		t.Fatalf("the unpatched runtime answers %s for the forged order word, want %s", got, want)
	}
	patched := &generatedUnit{dir: cu.g.dir, loaded: true, files: maps.Clone(cu.g.files)}
	patched.files["schema_cookread.luau"] = []byte(strings.Replace(string(src), anchor, "", 1))
	if got := runProbe(t, patched, probe, map[string][]byte{"wire": wire, "cook": cooked}); got != "ok" {
		t.Fatalf("the patched runtime no longer opens the clean cook (%s), so the control would go red for the wrong reason", got)
	}
	got := runProbe(t, patched, probe, map[string][]byte{"wire": wire, "cook": forged})
	if got == want {
		t.Fatalf("with the order-word test removed the forged cook still answers %s: the gate cannot see the order word", got)
	}
	t.Logf("order_word went red with the test removed: answered %q, Go answers %q", got, want)
}
