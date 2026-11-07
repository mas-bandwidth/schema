package luautable_test

// TestLuauTableUnboundedTextListsAndMapKeys holds docs/SPEC-TABLES.md §2.13.1
// (unbounded text-list elements and map keys) in the Luau table runtime, over
// the schema internal/codegen/gotable/unboundedtext_test.go holds Go to: every
// value's wire is computed in this test by schema-go's own engine
// (internal/tabletext reads the canonical JSON text, internal/tablewire
// encodes it), never typed by hand, and the Luau runtime loads that wire and
// saves it back byte for byte; the map's keys sort as the Go writer sorts
// them, and a repeated key gives the Go load's duplicate verdict.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
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

// luauUnboundedTextSchema is the schema the Go backend's own
// TestGoTableUnboundedTextListsAndMapKeys runs, so the Luau leg is held to the
// same construct and the same rows.
const luauUnboundedTextSchema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules []string(8)
    first [..4]?string(8)
    capped [..2]string(8)
    byname map[string(8)]int32
}
`

// luauUnboundedTextUnit loads the shared schema.
func luauUnboundedTextUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(luauUnboundedTextSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

// luauUnboundedTextField finds one field of a model instance by name.
func luauUnboundedTextField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s has no field %s", inst.Def.Name, name)
	return nil
}

// luauUnboundedTextList places one text-list value in the engine's instance,
// nil marking an absent optional element.
func luauUnboundedTextList(t *testing.T, m *tabletext.Model, inst *tabletext.Instance, name string, values []*string) {
	t.Helper()
	fv := luauUnboundedTextField(t, inst, name)
	fv.Elems = nil
	fv.Count = len(values)
	for _, value := range values {
		cell := m.ElementZero(fv.Def)
		if value != nil {
			rec := cell.Tab
			rec.Fields[0].Cell.Str = []byte(*value)
			rec.Fields[0].Count = len(*value)
			rec.Fields[0].Present = true
		}
		fv.Elems = append(fv.Elems, cell)
	}
}

// luauUnboundedTextMap places one string-keyed map value in the engine's
// instance; tablewire's writer sorts the entries, and keeps a repeated key
// (the duplicate row).
func luauUnboundedTextMap(t *testing.T, m *tabletext.Model, inst *tabletext.Instance, name string, keys []string, values []int32) {
	t.Helper()
	fv := luauUnboundedTextField(t, inst, name)
	fv.Entries = nil
	for i, key := range keys {
		entry := m.New(fv.Def.MapEntry)
		entry.Fields[0].Cell.Str = []byte(key)
		entry.Fields[0].Count = len(key)
		entry.Fields[1].Cell.I = int64(values[i])
		entry.Fields[1].Cell.U = uint64(int64(values[i]))
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: entry})
	}
}

// luauUnboundedTextWire is the Go writer's bytes for one engine instance: an
// instance is built through the model, which tablewire encodes with the same
// walk the Go backend's generated unit runs.
func luauUnboundedTextWire(t *testing.T, m *tabletext.Model, root string, fill func(*tabletext.Instance)) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	fill(inst)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: the oracle encode: %v", root, err)
	}
	return wire
}

// luauUnboundedTextGenerated emits the Luau type and table surfaces into a
// fresh directory, as this package's other probe tests do.
func luauUnboundedTextGenerated(t *testing.T, u *ir.Unit) *generatedUnit {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// luauUnboundedTextProbe loads each input wire, saves the value through
// schema_save.luau and prints the root, the load report's duplicate and malformed
// counters and the saved bytes. A refused or unsaveable value prints a
// recognizable failure instead.
func luauUnboundedTextProbe(g *generatedUnit) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local St = require(\"./schema_tables\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString(`local roots = { short = "Where", long = "Where", duplicate = "Where" }
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hexstr(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
for _, name in { "short", "long", "duplicate" } do
	local root = roots[name]
	local report = fresh()
	local v = load.load(root, inputs[name], report)
	if v == nil or report.refused > 0 then
		print(name .. ":REFUSED:" .. tostring(report.duplicate) .. ":" .. tostring(report.malformed))
	else
		local out = save.save(St.lookup(root), v)
		if out == nil then
			print(name .. ":SAVENIL:" .. tostring(report.duplicate) .. ":" .. tostring(report.malformed))
		else
			print(name .. ":" .. tostring(report.duplicate) .. ":" .. tostring(report.malformed) .. ":" .. hexstr(out))
		end
	end
end
`)
	return b.String()
}

// TestLuauTableUnboundedTextListsAndMapKeys holds the Luau runtime to the Go
// engine over the Go test's own schema. The short and long rows carry the
// construct in both spellings (an unbounded element, an optional unbounded
// element and an unbounded map key, each beside its bounded twin); the
// duplicate row is a writer's map body with one key repeated, built from the
// engine's own instance and handed to the engine's reader for the verdict.
func TestLuauTableUnboundedTextListsAndMapKeys(t *testing.T) {
	t.Parallel()
	u := luauUnboundedTextUnit(t)
	m := tabletext.NewModel(u)
	long := strings.Repeat("é", 35000)
	keyA := strings.Repeat("k", 70000) + "a"
	keyB := strings.Repeat("k", 70000) + "b"
	ptr := func(value string) *string { return &value }
	ptrs := func(values ...string) []*string {
		out := make([]*string, len(values))
		for i := range values {
			out[i] = ptr(values[i])
		}
		return out
	}
	inputs := map[string][]byte{
		"short": luauUnboundedTextWire(t, m, "Where", func(inst *tabletext.Instance) {
			luauUnboundedTextList(t, m, inst, "rules", ptrs("a", "b"))
			luauUnboundedTextList(t, m, inst, "first", []*string{ptr("x"), nil})
			luauUnboundedTextList(t, m, inst, "capped", ptrs("c"))
			luauUnboundedTextMap(t, m, inst, "byname", []string{"k2", "k1"}, []int32{2, 1})
		}),
		"long": luauUnboundedTextWire(t, m, "Where", func(inst *tabletext.Instance) {
			luauUnboundedTextList(t, m, inst, "rules", ptrs(long, "z"))
			luauUnboundedTextList(t, m, inst, "first", []*string{nil, ptr(long + "1")})
			luauUnboundedTextList(t, m, inst, "capped", ptrs(long+"2"))
			luauUnboundedTextMap(t, m, inst, "byname", []string{keyA, keyB}, []int32{7, 8})
		}),
	}

	// A REPEATED KEY: the engine's own writer emits the map body with one key
	// twice. The schema-go text and wire model at this base reads a bounded
	// key's capacity from the declaration, so the verdict is the engine's own
	// read of the string(8) twin's wire; the Go backend's test holds the two
	// spellings to the same bytes, and this test asserts it here.
	dupWire := luauUnboundedTextWire(t, m, "Where", func(inst *tabletext.Instance) {
		luauUnboundedTextMap(t, m, inst, "byname", []string{"k1", "k1"}, []int32{1, 2})
	})
	dupTwin := luauUnboundedTextWire(t, m, "Bounded", func(inst *tabletext.Instance) {
		luauUnboundedTextMap(t, m, inst, "byname", []string{"k1", "k1"}, []int32{1, 2})
	})
	if !bytes.Equal(dupWire, dupTwin) {
		t.Fatalf("the unbounded spelling moved a byte in the repeated-key wire\n got %x\nwant %x", dupWire, dupTwin)
	}
	inputs["duplicate"] = dupWire
	var goReport tabletext.Report
	back := m.New(m.Lookup("Bounded"))
	if ok, err := tablewire.Decode(m, back, dupTwin, &goReport); !ok || err != nil {
		t.Fatalf("the Go load of the repeated-key wire: ok=%v err=%v %+v", ok, err, goReport)
	}
	if goReport.Duplicate != 1 || goReport.Malformed {
		t.Fatalf("the Go load of the repeated-key wire reports %+v, want one duplicate and no damage", goReport)
	}

	g := luauUnboundedTextGenerated(t, u)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauUnboundedTextProbe(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	lines := strings.Split(out, "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), len(inputs), out)
	}
	got := map[string]string{}
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			t.Fatalf("the probe printed an unreadable line %q", line)
		}
		got[parts[0]] = line
	}
	for _, name := range []string{"short", "long"} {
		line, ok := got[name]
		if !ok {
			t.Errorf("%s: the probe printed no line", name)
			continue
		}
		want := name + ":0:false:" + hex.EncodeToString(inputs[name])
		if line != want {
			t.Errorf("%s: load and save do not reproduce the Go writer's bytes\n got  %s\n want %s", name, line, want)
		}
	}
	dup := got["duplicate"]
	wantDup := fmt.Sprintf("duplicate:%d:false:", goReport.Duplicate)
	if !strings.HasPrefix(dup, wantDup) {
		t.Errorf("duplicate: the Luau load's verdict does not match the Go load's %+v\n got  %s", goReport, dup)
	}
}
