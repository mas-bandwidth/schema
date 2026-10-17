package tablepack_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// wireListsRules builds a Lists wire whose rules field carries exactly the given
// elements, whatever its `[..4]` bound — an over-bound wire, for the read-side
// clamp cases. The fresh instance's other fields stay at their declared
// defaults (first and repos empty).
func wireListsRules(t *testing.T, m *tabletext.Model, elements []string) []byte {
	t.Helper()
	inst := m.New(m.Unit.Tables["Lists"])
	fv := &inst.Fields[0] // rules
	fv.Count = len(elements)
	fv.Elems = fv.Elems[:0]
	for _, e := range elements {
		rec := m.New(fv.Def.TextListElem())
		rec.Fields[0].Cell.Str = []byte(e)
		rec.Fields[0].Count = len(e)
		fv.Elems = append(fv.Elems, tabletext.Cell{Tab: rec})
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// The schema stringarrays_test.go's Go leg exercises: Lists (two text lists and
// an unbounded one), GoalList (a `json = "array"` root) and Holder (the array
// root nested by value).
const textListsSchema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
}

table GoalList | json = "array"
{
    goals []string(8)
}

table Holder
{
    g GoalList
    bases []?string(8)
}
`

// textListsGolden is the pinned wire of rules ["a","b"], first ["x",null] and an
// empty repos — the same bytes the Go leg pins in stringarrays_test.go.
const textListsGolden = "01010e060c0201610162020e0a0d0205030c017800010000c05351286a4f1267411d6f997fedd789ea0ce83094fde47c0300000000000000"

func textListsUnit(t *testing.T) (*compiler.Compiler, *tabletext.Model) {
	t.Helper()
	c := compiler.New()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(textListsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return c, tabletext.NewModel(u)
}

func compactJSON(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func TestEngineCarriesTextListsAndArrayRoots(t *testing.T) {
	t.Parallel()
	_, m := textListsUnit(t)
	golden, err := hex.DecodeString(textListsGolden)
	if err != nil {
		t.Fatal(err)
	}

	// unpack the pinned golden wire of the Lists root: exactly the Go leg's
	// text, with a silent report
	out := t.TempDir()
	report, err := tablepack.UnpackOneFile(m, "Lists", golden, out)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("the pinned wire did not read clean: %+v", report)
	}
	text, err := os.ReadFile(filepath.Join(out, "Lists.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, text); got != `{"rules":["a","b"],"first":["x",null],"repos":[]}` {
		t.Fatalf("unpack text\n got %s\nwant {\"rules\":[\"a\",\"b\"],\"first\":[\"x\",null],\"repos\":[]}", got)
	}

	// pack of that text gives the golden back byte for byte
	back, _, report, err := tablepack.Pack(m, "Lists", out)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("pack of the engine's own text was not silent: %+v", report)
	}
	if !bytes.Equal(back, golden) {
		t.Fatalf("pack moved bytes:\n got %x\nwant %x", back, golden)
	}

	// a GoalList packs from ["a","b"] and unpacks to it
	gdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gdir, "GoalList.json"), []byte(`["a","b"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	var gwire []byte
	gwire, _, report, err = tablepack.Pack(m, "GoalList", gdir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("GoalList pack not silent: %+v", report)
	}
	gout := t.TempDir()
	report, err = tablepack.UnpackOneFile(m, "GoalList", gwire, gout)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Silent() {
		t.Fatalf("GoalList unpack not silent: %+v", report)
	}
	gtext, err := os.ReadFile(filepath.Join(gout, "GoalList.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(t, gtext); got != `["a","b"]` {
		t.Fatalf("GoalList unpack text got %s, want [\"a\",\"b\"]", got)
	}

	// {"goals":["a"]} as a GoalList is refused by name, json_root_not_array
	baddir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baddir, "GoalList.json"), []byte(`{"goals":["a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, report, err = tablepack.Pack(m, "GoalList", baddir)
	if err == nil {
		t.Fatal("an object at an array root packed")
	}
	if !report.Malformed || report.Reason != "json_root_not_array: GoalList" {
		t.Fatalf("the refusal does not name json_root_not_array: %+v", report)
	}

	// five elements against [..4] clamp and count
	c1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(c1, "Lists.json"), []byte(`{"rules":["a","b","c","d","e"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, report, err = tablepack.Pack(m, "Lists", c1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Clamped != 1 {
		t.Fatalf("five elements against [..4]: clamped %d, want 1", report.Clamped)
	}

	// a nine-byte element against string(8) clamps and counts
	c2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(c2, "Lists.json"), []byte(`{"rules":["123456789"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, report, err = tablepack.Pack(m, "Lists", c2)
	if err != nil {
		t.Fatal(err)
	}
	if report.Clamped != 1 {
		t.Fatalf("a nine-byte element: clamped %d, want 1", report.Clamped)
	}

	// the read-side clamps NAME the field, as the Go leg does (§2.13): a wire
	// carrying five rules elements against [..4] and a wire carrying a
	// nine-byte element against string(8) each read clamped, counted, and with
	// report.Reason `clamped: Lists.rules`
	for _, c := range []struct {
		name     string
		elements []string
	}{
		{"count", []string{"a", "b", "c", "d", "e"}},
		{"length", []string{"123456789"}},
	} {
		wire := wireListsRules(t, m, c.elements)
		wout := t.TempDir()
		wrep, err := tablepack.UnpackOneFile(m, "Lists", wire, wout)
		if err != nil {
			t.Fatal(err)
		}
		if wrep.Clamped != 1 || wrep.Malformed {
			t.Fatalf("%s wire clamp: %+v", c.name, wrep)
		}
		if wrep.Reason != "clamped: Lists.rules" {
			t.Fatalf("%s wire clamp names no field: %q", c.name, wrep.Reason)
		}
	}
}
