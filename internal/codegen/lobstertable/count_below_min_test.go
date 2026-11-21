package lobstertable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A COUNTED ARRAY [Min..Max]T IS BORN AT ITS MINIMUM AND NEVER HOLDS LESS
// (docs/SPEC.md §4.6, §4.2 and §5.1 row 3; Go's ir.Field.BornCount): a fresh
// record carries Min element zeros and saves byte for byte as the
// reference's fresh instance, and the variable read refuses a count below
// Min as count_over_bound.
const countMinSchema = `package countmin

table Leaf { n int32 }

table Probe
{
    few    [2..4]uint32
    leaves [1..3]Leaf
    tail   int32 = 5
}
`

// countMinShortSchema is the same fields with no minimum: its writer can
// spell the short count the reader above must refuse.
const countMinShortSchema = `package countmin

table Leaf { n int32 }

table Probe
{
    few    [..4]uint32
    leaves [..3]Leaf
    tail   int32 = 5
}
`

func TestLobsterCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g := finiteGenerate(t, countMinSchema)
	m := tabletext.NewModel(g.u)
	fresh, err := tablewire.Encode(m, m.New(m.Lookup("Probe")))
	if err != nil {
		t.Fatal(err)
	}
	freshText, err := m.Write(m.New(m.Lookup("Probe")))
	if err != nil {
		t.Fatal(err)
	}
	// the short wires: the reference writer of the unminimized schema
	src := filepath.Join(t.TempDir(), "Short.schema")
	if err := os.WriteFile(src, []byte(countMinShortSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	su, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	sm := tabletext.NewModel(su)
	short := map[string][]byte{}
	for name, text := range map[string]string{"few": `{"few":[9],"leaves":[{"n":1}]}`} {
		inst := sm.New(sm.Lookup("Probe"))
		var rep tabletext.Report
		if !sm.Read(inst, []byte(text), &rep) {
			t.Fatalf("short fixture %s: %+v", name, rep)
		}
		w, err := tablewire.Encode(sm, inst)
		if err != nil {
			t.Fatal(err)
		}
		short[name] = w
	}
	out := finiteRun(t, g, `import .json
let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
print("born " + string(fslot(rec, "few").count) + " " + string(fslot(rec, "leaves").count))
let w, why = schema_rt.save_reasoned(rec)
print("save " + why + "|" + fhex(w))
let js, jwhy = schema_rt.json_write(rec)
print("json " + jwhy + "|" + js)
for(["few"]) f:
    let data = read_file(f)
    assert data
    let back, reason = schema_rt.load_strict("Probe", data, schema_rt.LoadReport {})
    print(f + " " + (if back: "read" else: reason))
`, short)
	got := strings.Join(out, "\n")
	if !strings.HasPrefix(got, "born 2 1\n") {
		t.Errorf("a fresh record's counts: %q, want born at the minimums 2 and 1", out[0])
	}
	if !strings.Contains(got, "save |"+hex.EncodeToString(fresh)+"\n") {
		t.Errorf("a fresh record's save is not the reference's fresh instance %x: %q", fresh, got)
	}
	if !strings.Contains(got, "json |"+strings.TrimSpace(string(freshText))) {
		t.Errorf("a fresh record's JSON is not the reference's:\n got %q\nwant %q", got, freshText)
	}
	if !strings.HasSuffix(got, "few count_over_bound") {
		t.Errorf("the variable read of a count below the minimum: %q, want it refused count_over_bound", got)
	}
}
