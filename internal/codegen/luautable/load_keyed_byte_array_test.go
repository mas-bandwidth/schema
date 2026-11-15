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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// keyedByteArraySchema is an enum-keyed byte array. Its [Slot]uint8 field is
// a 0-based buffer, like [N]uint8 (docs/SPEC-TABLES.md §2.4, §3.2;
// docs/PORT-LUAU.md, THE TYPE MAPPING).
const keyedByteArraySchema = `package probe

enum Slot { A, B, C, D }

table Bag
{
    slots [Slot]uint8
}
`

// keyedByteArrayGoOracle saves the same value and reads its own wire back
// with the generated Go strict load: the saved bytes and the read-back slots
// are the expected answers, never typed in here.
const keyedByteArrayGoOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestKeyedByteArrayOracle(t *testing.T) {
	v := Bag{}
	v.Slots = [4]uint8{0, 1, 255, 0}
	n := BagMeasure(&v)
	wire := make([]byte, n)
	if got := BagSave(&v, wire); got != n {
		t.Fatalf("save %d bytes, want %d", got, n)
	}
	fmt.Printf("save|%s\n", hex.EncodeToString(wire))
	region := make([]byte, BagLoadMeasure(wire))
	var report TableReport
	got := BagLoad(region, wire, &report)
	if got == nil {
		t.Fatalf("the generated Go oracle refused its own wire: %+v", report)
	}
	fmt.Printf("load|%d,%d,%d,%d\n", got.Slots[0], got.Slots[1], got.Slots[2], got.Slots[3])
}
`

// keyedByteArraySave builds the same value in Luau and saves it with the
// port's own save, printing the wire as hex.
const keyedByteArraySave = `local T = require("./BagTable")
local S = require("./schema_save")
local v = T.NewBag()
buffer.writeu8(v.Slots, 0, 0)
buffer.writeu8(v.Slots, 1, 1)
buffer.writeu8(v.Slots, 2, 255)
local b, why = S.save(T.BagDescriptor, v)
assert(b ~= nil, tostring(why))
local parts = table.create(buffer.len(b))
for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
print(table.concat(parts))
`

// keyedByteArrayLoad reads both wires through the port's public strict load
// and prints the slots by value. A raised error is not caught: the read must
// answer a value or a refusal (docs/SPEC.md, the strict-read contract).
const keyedByteArrayLoad = `local inputs = require("./inputs")
local load = require("./schema_load")
require("./BagTable")
local function read(name)
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = load.load("Bag", inputs[name], report)
	if v == nil then
		print(name .. "|refused")
		return
	end
	print(name .. "|" .. buffer.readu8(v.Slots, 0) .. "," .. buffer.readu8(v.Slots, 1) .. ","
		.. buffer.readu8(v.Slots, 2) .. "," .. buffer.readu8(v.Slots, 3))
end
read("luau")
read("go")
`

// TestLuauLoadKeyedByteArray pins docs/SPEC-TABLES.md §2.4, §3.2: an
// enum-keyed [E]uint8 field is a buffer, and the strict variable load lands
// each keyed slot through the same put an array body uses. The value saves to
// the Go oracle's bytes, reads back to the Go oracle's value, and the oracle's
// own bytes read to that same value.
func TestLuauLoadKeyedByteArray(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Bag.schema")
	if err := os.WriteFile(schemaPath, []byte(keyedByteArraySchema), 0o644); err != nil {
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
	luauDir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luauDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(keyedByteArraySave), 0o644); err != nil {
		t.Fatal(err)
	}
	luauWire, err := hex.DecodeString(strings.TrimSpace(string(luaurun.Run(t, luauDir, "main.luau", nil))))
	if err != nil {
		t.Fatalf("the Luau saved wire: %v", err)
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
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	goFiles["oracle_test.go"] = []byte(keyedByteArrayGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestKeyedByteArrayOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		name, value, ok := strings.Cut(line, "|")
		if !ok || (name != "save" && name != "load") {
			continue
		}
		want[name] = value
	}
	if want["save"] == "" || want["load"] == "" {
		t.Fatalf("the oracle printed no save or load line:\n%s", out)
	}
	goWire, err := hex.DecodeString(want["save"])
	if err != nil {
		t.Fatalf("the oracle's wire: %v", err)
	}
	if string(luauWire) != string(goWire) {
		t.Fatalf("the Luau save is not the Go oracle's bytes: Luau %x, Go %x", luauWire, goWire)
	}

	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(keyedByteArrayLoad), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := luaurun.Run(t, luauDir, "main.luau", map[string][]byte{"luau": luauWire, "go": goWire})
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		name, value, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected Luau line %q", line)
		}
		got[name] = value
	}
	for _, name := range []string{"luau", "go"} {
		if got[name] != want["load"] {
			t.Errorf("%s wire: Luau read %q, the Go oracle read %q", name, got[name], want["load"])
		}
	}
}
