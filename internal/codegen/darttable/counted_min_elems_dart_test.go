package darttable

// A counted [A..B] array is BORN at A elements, each at its default, and the
// table-wire writer elides it exactly at that state (docs/SPEC-TABLES.md §2.3,
// internal/tablewire encodeArray): the live count is A and every element
// writes nothing — a table element whose body is the terminator alone (judged
// with the id table rolled back), a union element that is None, a scalar at
// its default. Selecting a union arm whose payload is a counted [A..B] holds
// A elements, not none. The bytes are the independent engine's
// (internal/tablewire), the same bytes in every language: born, one over the
// minimum, and one element set must each match it.
//
// The negative control plants the old judgement (an array rides whenever it
// is not empty) in the real artifact, and the born rows must then differ.
//
// A FUNCTIONAL TEST: it runs the Dart VM. No clock, no socket, no network.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const dartCountedMinSchema = `package probe

table E
{
    n uint32
}

union U
{
    a uint32
    b int32
}

union P
{
    few   [2..4]uint32
    other int32
}

table Rt
{
    es [2..4]E
}

table Ru
{
    us [2..4]U
}

table Rs
{
    xs [2..4]uint32
}

table Ra
{
    pick P
}
`

// one case: the table, the engine's text for the value, and the Dart lines
// that build the same value in `v` from a freshly constructed one
type dartCountedMinCase struct {
	id, table, text, build string
}

var dartCountedMinCases = []dartCountedMinCase{
	{"table_born", "Rt", `{"es":[{},{}]}`, ``},
	{"table_three", "Rt", `{"es":[{},{},{}]}`, `v.es.add(w.E());`},
	{"table_one_set", "Rt", `{"es":[{},{"n":7}]}`, `v.es[1].n = 7;`},
	{"union_born", "Ru", `{"us":[{},{}]}`, ``},
	{"union_three", "Ru", `{"us":[{},{},{}]}`, `v.us.add(w.U());`},
	{"union_one_set", "Ru", `{"us":[{},{"a":7}]}`, `v.us[1].armTag = 1; v.us[1].a = 7;`},
	{"scalar_born", "Rs", `{"xs":[0,0]}`, ``},
	{"scalar_three", "Rs", `{"xs":[0,0,0]}`, `v.xs.add(0);`},
	{"scalar_one_set", "Rs", `{"xs":[0,7]}`, `v.xs[1] = 7;`},
	{"arm_selected", "Ra", `{"pick":{"few":[0,0]}}`, `v.pick.armTag = 1;`},
	{"arm_one_set", "Ra", `{"pick":{"few":[0,7]}}`, `v.pick.armTag = 1; v.pick.few[1] = 7;`},
}

func dartCountedMinEngine(t *testing.T) map[string]string {
	t.Helper()
	u := unitFrom(t, dartCountedMinSchema)
	m := tabletext.NewModel(u)
	want := map[string]string{}
	for _, c := range dartCountedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		wire, err := tablewire.EncodeVariable(m, v)
		if err != nil {
			t.Fatalf("%s: the engine did not encode: %v", c.id, err)
		}
		want[c.id] = hex.EncodeToString(wire)
	}
	return want
}

func dartCountedMinRun(t *testing.T, undermine func(map[string][]byte)) map[string]string {
	t.Helper()
	u := unitFrom(t, dartCountedMinSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	if undermine != nil {
		undermine(files)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var b strings.Builder
	b.WriteString("import 'ProbeWire.dart' as w;\nString hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();\nvoid main() {\n")
	for _, c := range dartCountedMinCases {
		fmt.Fprintf(&b, "  { final v = w.%s(); %s print('%s ${hex(w.%sWireSave(v))}'); }\n", c.table, c.build, c.id, dartName(c.table))
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("an unexpected probe line: %q", line)
		}
		got[parts[0]] = parts[1]
	}
	return got
}

func TestDartCountedMinElementsElideAtBornState(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	want := dartCountedMinEngine(t)
	got := dartCountedMinRun(t, nil)
	for _, c := range dartCountedMinCases {
		if got[c.id] != want[c.id] {
			t.Errorf("%s: the Dart bytes differ from the engine's\n got %s\nwant %s", c.id, got[c.id], want[c.id])
		}
	}
}

func TestDartCountedMinElementsElideNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	want := dartCountedMinEngine(t)
	born := regexp.MustCompile(`(?m)^(\s*)if \(([\w.]+)\.length != 2 \|\| .*\) \{$`)
	got := dartCountedMinRun(t, func(files map[string][]byte) {
		planted := 0
		for name, data := range files {
			if text := born.ReplaceAllString(string(data), "${1}if (${2}.isNotEmpty) {"); text != string(data) {
				files[name] = []byte(text)
				planted++
			}
		}
		if planted == 0 {
			t.Fatal("no generated file carried the birth judgement to plant out")
		}
	})
	for _, id := range []string{"table_born", "union_born", "scalar_born"} {
		if got[id] == want[id] {
			t.Errorf("%s: with the birth judgement planted out, the Dart bytes still match the engine's", id)
		}
	}
}
