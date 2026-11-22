package lobstertable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterFlagsPastWidthRefuse holds a flags mask to its reader's width W
// (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the newer
// reader reads old data, and never the other way. A newer build's variable
// file with its appended bit set, a narrower unsigned field widened into the
// mask with a bit past W, and a fixed record with a stored bit past W each
// refuse the whole load out_of_range; the twins inside W read. The variable
// save and the checked fixed save refuse a mask past W invalid_value, so a
// same-build round trip never writes what its own reader refuses. Under the
// old rule (a mask was range-free) every refusal here read.
func TestLobsterFlagsPastWidthRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	wire := func(src, text string) []byte {
		t.Helper()
		m := tabletext.NewModel(flagsPastUnit(t, src))
		inst := m.New(m.Lookup("Probe"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("the Go engine does not read %s: %+v", text, rep)
		}
		b, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	const newer = "package flagspast\nflags Cap { Trace, Verbose, Extra }\ntable Probe { cap Cap }\n"
	const narrow = "package flagspast\ntable Probe { cap uint8 }\n"
	inputs := map[string][]byte{
		"past":       wire(newer, `{"cap":["Trace","Extra"]}`),
		"inside":     wire(newer, `{"cap":["Trace","Verbose"]}`),
		"widepast":   wire(narrow, `{"cap":5}`),
		"wideinside": wire(narrow, `{"cap":3}`),
	}
	g := finiteGenerate(t, "package flagspast\nflags Cap { Trace, Verbose }\ntable Probe { cap Cap }\n")
	got := strings.Join(finiteRun(t, g, flagsPastVariableMain, inputs), "\n")
	if want := "past out_of_range\ninside read 3\nwidepast out_of_range\nwideinside read 3\nsave invalid_value"; got != want {
		t.Errorf("the variable form:\n got %q\nwant %q", got, want)
	}

	u := flagsPastUnit(t, "package flagspastfixed\nflags Cap { Trace, Verbose }\nfixed table Row { cap Cap }\n")
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	files["main.lobster"] = []byte(flagsPastFixedMain)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
	if want := "diffs 1\nload_past -1 out_of_range\nload_inside 1 3\nsave_checked -1 invalid_value"; out != want {
		t.Errorf("the fixed form:\n got %q\nwant %q", out, want)
	}
}

func flagsPastUnit(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

const flagsPastVariableMain = `for(["past", "inside", "widepast", "wideinside"]) f:
    let data = read_file(f)
    assert data
    let back, reason = schema_rt.load_strict("Probe", data, schema_rt.LoadReport {})
    print(f + " " + (if back: "read " + string(fslot(back, "cap").cell.i) else: reason))
let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
fslot(rec, "cap").cell.i = 5
let w, why = schema_rt.save_reasoned(rec)
print("save " + why)
`

const flagsPastFixedMain = `import .ProbeTable
import .schema_runtime

def image(mask:int) -> string:
    let v = flagspastfixed.NewRow()
    v.Cap = mask
    return flagspastfixed.RowFixedSave([v])

let low = image(1)
let both = image(3)
var at = -1
var diffs = 0
for(length(low)) i:
    if low[i] != both[i]:
        at = i
        diffs++
if length(low) != length(both): diffs = -1
print("diffs {diffs}")
let forged = write_int8_le(copy(low), at, low[at] | 4)
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let back = [flagspastfixed.NewRow()]
let r1 = schema_rt.TableReport {}
let n1 = flagspastfixed.RowFixedLoad(back, forged, plan, r1)
print("load_past {n1} " + r1.reason)
let r2 = schema_rt.TableReport {}
let n2 = flagspastfixed.RowFixedLoad(back, both, plan, r2)
print("load_inside {n2} {back[0].Cap}")
let bad = flagspastfixed.NewRow()
bad.Cap = 5
let n, _, why = flagspastfixed.RowFixedSaveChecked([bad], schema_rt.zeros(flagspastfixed.RowFixedMeasure(1)))
print("save_checked {n} " + why)
`
