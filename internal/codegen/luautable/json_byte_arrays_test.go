package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonByteArraysSchema carries a uint8 array of each shape the JSON form can
// see: fixed [N], counted [..N] and enum-keyed [E]. Each is a buffer, 0-based,
// in the Luau type mapping (docs/PORT-LUAU.md, THE TYPE MAPPING).
const jsonByteArraysSchema = `package probe

enum Team
{
    Red
    Blue
    Green
}

table Box
{
    a [4]uint8
    c [..4]uint8
    k [Team]uint8
}
`

// jsonByteArraysGoOracle prints the canonical text, the wire and the landed
// storage of the Box defaults and of one filled value. The defaults and the
// filled value are the two texts the Luau probe writes; FromJSON of each text
// answers the storage a read lands, so no expected byte is typed into the test.
const jsonByteArraysGoOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestJsonByteArraysOracle(t *testing.T) {
	var defaults Box
	printValue(t, "defaults", &defaults)
	filled := Box{
		A:      [4]uint8{1, 2, 3, 4},
		C:      [4]uint8{5, 6, 7, 99},
		CCount: 3,
		K:      [3]uint8{9, 10, 11},
	}
	printValue(t, "filled", &filled)
}

func printValue(t *testing.T, name string, v *Box) {
	n := BoxToJSONMeasure(v)
	if n < 0 {
		t.Fatalf("%s: text measure %d", name, n)
	}
	text := make([]byte, n)
	if BoxToJSON(v, text) != n {
		t.Fatalf("%s: text write", name)
	}
	fmt.Printf("%s.text=%s\n", name, hex.EncodeToString(text))
	wire := make([]byte, BoxMeasure(v))
	if BoxSave(v, wire) != int64(len(wire)) {
		t.Fatalf("%s: save", name)
	}
	fmt.Printf("%s.wire=%s\n", name, hex.EncodeToString(wire))

	var b BoxBuilder
	if !b.Init() {
		t.Fatalf("%s: builder init", name)
	}
	defer b.Shutdown()
	var report TableReport
	if !BoxFromJSON(&b, text, &report) {
		t.Fatalf("%s: FromJSON %+v", name, report)
	}
	if !b.Lock() {
		t.Fatalf("%s: builder lock", name)
	}
	got := b.AsConst()
	fmt.Printf("%s.landed.a=%s\n", name, hex.EncodeToString(got.A[:]))
	fmt.Printf("%s.landed.c=%s\n", name, hex.EncodeToString(got.C[:]))
	fmt.Printf("%s.landed.count=%d\n", name, got.CCount)
	fmt.Printf("%s.landed.k=%s\n", name, hex.EncodeToString(got.K[:]))
	back := make([]byte, BoxMeasure(got))
	if BoxSave(got, back) != int64(len(back)) {
		t.Fatalf("%s: landed save", name)
	}
	fmt.Printf("%s.landed.wire=%s\n", name, hex.EncodeToString(back))
}
`

// jsonByteArraysProbe writes the two texts from the constructor's default and
// a filled value, then reads each text back and prints the landed storage, the
// wire it saves and its type names (docs/SPEC-TABLES.md §16.1, §16.2).
const jsonByteArraysProbe = `require("./BoxTable")
local T = require("./BoxTable")
local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local function bytes(...)
	local t = { ... }
	local b = buffer.create(#t)
	for i = 1, #t do buffer.writeu8(b, i - 1, t[i]) end
	return b
end
local function write(name, v)
	local text = json.ToJson("Box", v)
	if text == nil then print(name .. ".text=refused"); return end
	print(name .. ".text=" .. hex(text))
end
local defaults = T.NewBox()
write("defaults", defaults)
local filled = T.NewBox()
filled.A = bytes(1, 2, 3, 4)
filled.C = bytes(5, 6, 7, 99)
filled.CCount = 3
filled.K = bytes(9, 10, 11)
write("filled", filled)
local function land(name)
	local v = json.FromJson("Box", inputs[name])
	if v == nil then print(name .. ".landed=refused"); return end
	print(name .. ".landed.a=" .. hex(v.A))
	print(name .. ".landed.c=" .. hex(v.C))
	print(name .. ".landed.count=" .. tostring(v.CCount))
	print(name .. ".landed.k=" .. hex(v.K))
	print(name .. ".landed.types=" .. typeof(v.A) .. "," .. typeof(v.C) .. "," .. typeof(v.K))
	local wire = save.save(St.lookup("Box"), v)
	print(name .. ".landed.wire=" .. hex(wire))
end
land("defaults")
land("filled")
`

// TestLuauJsonByteArrays: ToJson writes the Go engine's canonical text for a
// [N]uint8, a [..N]uint8 and an [E]uint8 at the constructor's defaults and when
// filled; FromJson of that text lands a buffer of the same bytes, with the
// live count for the counted shape; and Save of the landed value equals the Go
// engine's wire (docs/PORT-LUAU.md, THE TYPE MAPPING; docs/SPEC-TABLES.md
// §16.2 rows [N]T, [..N]T, [E]T).
func TestLuauJsonByteArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(jsonByteArraysSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
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
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	goTables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, goTables)
	runtime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	goFiles["oracle_test.go"] = []byte(jsonByteArraysGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestJsonByteArraysOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch {
		case strings.HasSuffix(key, ".text"), strings.HasSuffix(key, ".wire"),
			strings.HasPrefix(key, "defaults.landed."), strings.HasPrefix(key, "filled.landed."):
			want[key] = value
		}
	}
	for _, name := range []string{"defaults", "filled"} {
		for _, suffix := range []string{".text", ".wire", ".landed.a", ".landed.c", ".landed.count", ".landed.k", ".landed.wire"} {
			if _, ok := want[name+suffix]; !ok {
				t.Fatalf("the Go oracle printed no %s%s:\n%s", name, suffix, out)
			}
		}
	}

	inputs := map[string][]byte{}
	for _, name := range []string{"defaults", "filled"} {
		text, err := hex.DecodeString(want[name+".text"])
		if err != nil {
			t.Fatalf("the oracle's %s text: %v", name, err)
		}
		inputs[name] = text
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonByteArraysProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut, err := runLuauErr(t, g.dir, "main.luau", inputs)
	if err != nil {
		t.Fatalf("the Luau probe failed: %v\n%s", err, luauOut)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		got[key] = value
	}
	for _, name := range []string{"defaults", "filled"} {
		for _, suffix := range []string{".text", ".landed.a", ".landed.c", ".landed.count", ".landed.k", ".landed.wire"} {
			if got[name+suffix] != want[name+suffix] {
				t.Errorf("%s%s: Luau %q, Go %q", name, suffix, got[name+suffix], want[name+suffix])
			}
		}
		if got[name+".landed.types"] != "buffer,buffer,buffer" {
			t.Errorf("%s landed types %q, want buffer,buffer,buffer", name, got[name+".landed.types"])
		}
		if got[name+".landed.wire"] != want[name+".wire"] {
			t.Errorf("%s: Save of the landed value is %q, the Go engine's wire %q", name, got[name+".landed.wire"], want[name+".wire"])
		}
	}
}
