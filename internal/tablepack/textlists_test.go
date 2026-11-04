package tablepack_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// wireListsRules builds a Lists wire whose rules field carries exactly the given
// elements, whatever its `[..4]` bound — an over-bound wire, for the read-side
// refusal cases. The fresh instance's other fields stay at their declared
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
	if !report.Refused || report.Reason != "json_root_not_array: GoalList" {
		t.Fatalf("the refusal does not name json_root_not_array: %+v", report)
	}

	// five elements against [..4] refuse count_over_bound, and a nine-byte
	// element against string(8) refuses length_over_bound; four elements of
	// eight bytes each, the bounds exactly, pack
	for _, c := range []struct{ text, reason string }{
		{`{"rules":["a","b","c","d","e"]}`, "count_over_bound"},
		{`{"rules":["123456789"]}`, "length_over_bound"},
		{`{"rules":["12345678","b","c","d"]}`, ""},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Lists.json"), []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, report, err = tablepack.Pack(m, "Lists", dir)
		if c.reason == "" {
			if err != nil || !report.Silent() {
				t.Fatalf("%s: the bounds exactly: %+v %v", c.text, report, err)
			}
			continue
		}
		if err == nil || !report.Refused || !strings.HasPrefix(report.Reason, c.reason) {
			t.Fatalf("%s: want a %s refusal, got %+v %v", c.text, c.reason, report, err)
		}
	}

	// the wire refuses the same two conditions: a wire carrying five rules
	// elements against [..4] and a wire carrying a nine-byte element against
	// string(8) each refuse the read, and nothing is written; the wire of
	// four eight-byte elements reads
	for _, c := range []struct {
		name     string
		elements []string
		reason   string
	}{
		{"count", []string{"a", "b", "c", "d", "e"}, tablewire.ReasonCountOverBound},
		{"length", []string{"123456789"}, tablewire.ReasonLengthOverBound},
		{"exact", []string{"12345678", "b", "c", "d"}, ""},
	} {
		wire := wireListsRules(t, m, c.elements)
		wout := t.TempDir()
		wrep, err := tablepack.UnpackOneFile(m, "Lists", wire, wout)
		if c.reason == "" {
			if err != nil || !wrep.Silent() {
				t.Fatalf("%s wire: %+v %v", c.name, wrep, err)
			}
			continue
		}
		if err == nil || !tablewire.ReadRefusal(err) || tablewire.RefusalReason(err) != c.reason {
			t.Fatalf("%s wire: want %s, got %+v %v", c.name, c.reason, wrep, err)
		}
		if entries, _ := os.ReadDir(wout); len(entries) != 0 {
			t.Fatalf("%s wire: a refused read wrote %d entries", c.name, len(entries))
		}
	}
}
