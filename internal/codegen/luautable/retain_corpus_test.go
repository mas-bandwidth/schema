package luautable_test

// THE RETAIN CORPUS (docs/SPEC-TABLES.md §6.6): every `retain` row of
// testdata/conformance/tables/MANIFEST.txt, run through the Luau runtime's
// LoadRetain and SaveRetain exactly as the conformance driver runs it
// (test/conformance/luau/main.luau, surfaceRetain), held to the row's
// counters and, where the row pins one, to the C++ reference's saved bytes.
//
// It is the witness for the retaining save's defects the leg found: a union
// field's arms looked up on the FIELD line (a nil walk), an enum-keyed array
// located as a plain one, a record's u32 length placeholder written as four
// parts (three stray bytes per framed body), ids interned in emit order
// rather than write order, an enum reference copied by its stale index, and
// no retained-id capacity at all (retain_ids_short).

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type retainRow struct {
	name, root, wire, capacity, ids, counters, save string
}

// retainRows reads the manifest's tblrt1 `retain` rows, each
// `retain <case> <unit> <root> <wire> <capacity> <ids> <counters> <save lost> <save|->`.
func retainRows(t *testing.T) []retainRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []retainRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) < 10 || x[0] != "retain" || x[2] != "tblrt1" {
			continue
		}
		rows = append(rows, retainRow{name: x[1], root: x[3], wire: x[4], capacity: x[5], ids: x[6], counters: x[7] + " " + x[8], save: x[9]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no tblrt1 retain row")
	}
	return rows
}

// TestLuauRetainAgreesWithTheCorpus runs every retain row.
func TestLuauRetainAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths(prefix([]string{"test/tables/RT1.schema"}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range retainRows(t) {
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		ids := "131072"
		if row.ids != "full" {
			ids = row.ids
		}
		probe := fmt.Sprintf(`--!strict
require("./RT1Table")
local R = require("./schema_retain")
local inputs = require("./inputs")
local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
end
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local capacity = 1048576
if %q == "short" then
	local _, probe = R.LoadRetain(%q, inputs.wire, report(), capacity, %s)
	capacity = math.max(0, probe.used - 1)
end
local r = report()
local v, store = R.LoadRetain(%q, inputs.wire, r, capacity, %s)
assert(v ~= nil and not r.malformed, "the retained load refused")
local lost = r.retainLost
local out = R.SaveRetain(%q, v, store, r)
assert(out ~= nil, "the retaining save refused")
print(string.format("%%d,%%d,%%d %%d", r.retained, lost, r.unknown, r.retainLost - lost))
print(hex(out :: buffer))
`, row.capacity, row.root, ids, row.root, ids, row.root)
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire}))), "\n")
		if len(lines) != 2 {
			t.Fatalf("%s: the probe printed %d lines, want 2", row.name, len(lines))
		}
		if lines[0] != row.counters {
			t.Errorf("%s: counters %q, the manifest's %q", row.name, lines[0], row.counters)
		}
		if row.save == "-" {
			continue
		}
		want, err := os.ReadFile(filepath.Join(root, row.save))
		if err != nil {
			t.Fatal(err)
		}
		got, err := hex.DecodeString(lines[1])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: the retaining save wrote %d bytes, the reference's %s is %d (or differs)", row.name, len(got), row.save, len(want))
		}
	}
}

// THE WRITE-ORDER WITNESS. A retained record inside `child` interns an id the
// reader cannot name BEFORE the known fields after it write theirs: `mode`'s
// enum variant reference, `pick`'s arm and the arm's own retained field. The
// Luau save must renumber every one of them in the order the bytes are
// written, as Go's EncodeRetain does (the arm's id before the arm's new one), and must never copy an enum reference
// by its old index.
const retainOrderOld = `package order
enum Mode { Low, High }
table Child { n int32 }
table Box { n int32 }
union Pick { parcel Box
 tally int32 }
table Root { child Child
 mode Mode
 pick Pick }
`

const retainOrderNew = `package order
enum Mode { Low, High }
table Child { n int32
 later int32 }
table Box { n int32
 deep int32 }
union Pick { parcel Box
 tally int32 }
table Root { child Child
 mode Mode
 pick Pick }
`

const retainOrderText = `{"child":{"n":1,"later":7},"mode":"High","pick":{"parcel":{"n":2,"deep":9}}}`

func retainOrderUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "order.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLuauRetainSaveRenumbersInWriteOrder(t *testing.T) {
	t.Parallel()
	newModel := tabletext.NewModel(retainOrderUnit(t, retainOrderNew))
	inst := newModel.New(newModel.Lookup("Root"))
	var report tabletext.Report
	if !newModel.Read(inst, []byte(retainOrderText), &report) {
		t.Fatalf("schema-go's text read: %+v", report)
	}
	wire, err := tablewire.Encode(newModel, inst)
	if err != nil {
		t.Fatal(err)
	}
	old := retainOrderUnit(t, retainOrderOld)
	oldModel := tabletext.NewModel(old)
	back := oldModel.New(oldModel.Lookup("Root"))
	retain := &tablewire.Retain{Capacity: 1 << 16, IdCapacity: 1 << 13}
	var goReport tabletext.Report
	if ok, err := tablewire.DecodeRetain(oldModel, back, wire, retain, &goReport); !ok || err != nil {
		t.Fatalf("schema-go's retained decode: %v %+v", err, goReport)
	}
	want, err := tablewire.EncodeRetain(oldModel, back, retain, &goReport)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(old)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(old)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	probe := `--!strict
require("./orderTable")
local R = require("./schema_retain")
local inputs = require("./inputs")
local r: any = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = R.LoadRetain("Root", inputs.wire, r, 65536)
assert(v ~= nil, "the retained load refused")
local out = R.SaveRetain("Root", v, store, r)
assert(out ~= nil, "the retaining save refused")
local parts = {}
for i = 0, buffer.len(out :: buffer) - 1 do
	parts[#parts + 1] = string.format("%02x", buffer.readu8(out :: buffer, i))
end
print(r.retained .. " " .. table.concat(parts))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire})))
	if len(f) != 2 || f[0] != "2" {
		t.Fatalf("the probe printed %q, want two retained records and the bytes", f)
	}
	got, err := hex.DecodeString(f[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the Luau retaining save is\n%x\nGo's EncodeRetain is\n%x", got, want)
	}
}
