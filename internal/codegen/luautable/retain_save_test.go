package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The old unit cannot name Child.extra or Root.future. The newer build writes
// both, and a retaining load keeps them beside the value; the retaining save
// must write them back where they came from so the newer build reads its own
// fields again (docs/SPEC-TABLES.md §6.6).
const saveRetainOldSchema = `package probe
table Child { n int32 }
union Choice { child Child
 empty }
table Root { child Child
 choice Choice
 ptr Child }
`

const saveRetainNewSchema = `package probe
table Child { n int32
 extra string(32) }
union Choice { child Child
 empty }
table Root { child Child
 choice Choice
 ptr Child
 future string(32) }
`

func saveRetainCompile(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

func saveRetainWire(t *testing.T, u *ir.Unit, json string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(json), &report) {
		t.Fatalf("the newer value does not read: %+v", report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// saveRetainRunLuau loads the newer wire with the OLD unit's LoadRetain and
// saves it with SaveRetain, answering the saved bytes.
func saveRetainRunLuau(t *testing.T, g *generatedUnit, wire []byte) []byte {
	t.Helper()
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString(luaurun.HexProbe)
	b.WriteString("local retain = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString(`
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = retain.LoadRetain("Root", inputs.wire, report, 1048576)
if v == nil then print("LOADNIL") return end
local saved, refused = retain.SaveRetain("Root", v, store, report)
if saved == nil then print("SAVENIL") return end
hex(saved)
`)
	probe := b.String()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
	lines := strings.Split(out, "\n")
	out = lines[len(lines)-1]
	if out == "LOADNIL" || out == "SAVENIL" {
		t.Fatalf("SaveRetain refused a value LoadRetain answered: %s", out)
	}
	got, err := hex.DecodeString(out)
	if err != nil {
		t.Fatalf("the probe did not print hex: %v (%q)", err, out)
	}
	return got
}

// saveRetainGoReadsBack has the NEW unit's schema-go engine read the saved
// wire and save it again, proving the fields unknown to Luau came back and the
// newer build reads its own fields (§6.6).
func saveRetainGoReadsBack(t *testing.T, u *ir.Unit, wire []byte) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, v, wire, &report); !ok || err != nil || report.Malformed {
		t.Fatalf("the newer build could not read the saved wire: %v %v %+v", ok, err, report)
	}
	back, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func TestLuauSaveRetainWritesWhatItKept(t *testing.T) {
	t.Parallel()
	oldUnit := saveRetainCompile(t, saveRetainOldSchema)
	newUnit := saveRetainCompile(t, saveRetainNewSchema)

	oldModel := tabletext.NewModel(oldUnit)
	oldValue := oldModel.New(oldModel.Lookup("Root"))
	keep := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	wire := saveRetainWire(t, newUnit, `{"child":{"n":7,"extra":"node kept"},"ptr":{"n":9},"future":"root kept"}`)
	var loadReport tabletext.Report
	ok, err := tablewire.DecodeRetain(oldModel, oldValue, wire, keep, &loadReport)
	if !ok || err != nil || loadReport.Malformed {
		t.Fatalf("the oracle read failed: %v %v %+v", ok, err, loadReport)
	}
	if loadReport.Retained < 1 {
		t.Fatalf("the oracle kept nothing from a newer wire: %+v", loadReport)
	}
	var saveReport tabletext.Report
	want, err := tablewire.EncodeRetain(oldModel, oldValue, keep, &saveReport)
	if err != nil {
		t.Fatalf("the oracle save failed: %v", err)
	}

	g := generateFiles(t, map[string]string{"Probe.schema": saveRetainOldSchema})
	got := saveRetainRunLuau(t, g, wire)
	if !bytes.Equal(got, want) {
		t.Fatalf("SaveRetain wrote %x\nwant (tablewire) %x\nload report %+v save report %+v", got, want, loadReport, saveReport)
	}

	// THE NEWER BUILD READS ITS OWN FIELDS BACK (§6.6): the generated Go of
	// the new unit loads the saved wire and saves it again byte for byte.
	back := saveRetainGoReadsBack(t, newUnit, got)
	if !bytes.Equal(back, got) {
		t.Fatalf("the newer build did not read its fields back:\nsaved %x\nreadback %x", got, back)
	}
}
