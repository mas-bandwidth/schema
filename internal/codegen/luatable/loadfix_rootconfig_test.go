package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadFixRootConfigWire builds the divergence wire
// TestLuaWireFuzzAgreesWithTheGoEngine reported at the unchanged base:
//
//	seed 0x13ba79ffad7d6fb8 unit tabledemo root RootConfig case root_full
//
// The third mutation writes -0.0 into profiles[0].ratings, a plain float32
// array whose every element is otherwise its zero default. RootConfig is a
// fixed table, so the wire is the positive root_full instance in its form-3
// file (testdata/conformance/tables/wire/root_full.bin) with that one
// element mutated and re-encoded by the engine's fixed writer: a form-1
// file at this root is previous_form to every reader (§3.4).
func loadFixRootConfigWire(t *testing.T, m *tabletext.Model, def *ir.Struct) []byte {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "wire", "root_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inst := m.New(def)
	var rep tabletext.Report
	if ok, err := tablewire.Decode(m, inst, data, &rep); !ok || err != nil {
		t.Fatalf("decode root_full: ok=%v err=%v report=%+v", ok, err, rep)
	}
	profiles := fieldByName(t, inst, "profiles")
	profile := profiles.Elems[0].Tab
	ratings := fieldByName(t, profile, "ratings")
	ratings.Elems[0].F = math.Copysign(0, -1)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("encode mutated root_full: %v", err)
	}
	return wire
}

// fieldByName answers one field of an instance by its schema name.
func fieldByName(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("instance %s has no field %s", inst.Def.Name, name)
	return nil
}

// loadFixRootConfigCounts is the report's six counters.
type loadFixRootConfigCounts struct {
	unknown, kindChanged, widened, outOfRange, repeatedKey int
	damaged                                            bool
}

// loadFixRootConfigProbe loads one wire with the generated fixed form and
// re-saves it with FixedSave, printing the counters and the saved hex. A
// fixed read moves none of the variable form's counters, so the counter
// fields are the zeros both sides owe.
func loadFixRootConfigProbe(module, root string) string {
	return `local T = require("` + module + `")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local ok, rows = pcall(T.FixedLoad, "` + root + `", inputs.wire)
local v = ok and rows[1] or nil
local saved = "none"
if v ~= nil then
  local sok, b = pcall(T.FixedSave, "` + root + `", rows)
  if sok and b ~= nil then saved = hex(b) end
end
print(table.concat({ v ~= nil and 1 or 0, 0, 0, 0, 0, 0,
  "false", v ~= nil and 0 or 1, saved }, "|"))
`
}

// loadFixRootConfigParse reads one probe line.
func loadFixRootConfigParse(line string) (loadFixRootConfigCounts, []byte, error) {
	var c loadFixRootConfigCounts
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 9 {
		return c, nil, fmt.Errorf("probe line has %d fields, want 9: %q", len(f), line)
	}
	if f[0] != "1" {
		return c, nil, fmt.Errorf("the Lua load returned no root: %q", line)
	}
	if f[7] != "0" {
		return c, nil, fmt.Errorf("the Lua load refused the root: %q", line)
	}
	n := []*int{&c.unknown, &c.kindChanged, &c.widened, &c.outOfRange, &c.repeatedKey}
	for i, p := range n {
		if _, err := fmt.Sscanf(f[1+i], "%d", p); err != nil {
			return c, nil, fmt.Errorf("probe line %q field %d: %w", line, 1+i, err)
		}
	}
	c.damaged = f[6] == "true"
	if f[8] == "none" {
		return c, nil, fmt.Errorf("the Lua save refused the value it loaded: %q", line)
	}
	saved, err := hex.DecodeString(f[8])
	if err != nil {
		return c, nil, fmt.Errorf("probe line %q saved bytes: %w", line, err)
	}
	return c, saved, nil
}

// loadFixRootConfigEngine reads one wire with the compiler's engine, as the
// wire-fuzz differential does, and returns its counters and re-saved bytes.
func loadFixRootConfigEngine(m *tabletext.Model, def *ir.Struct, data []byte) (loadFixRootConfigCounts, []byte, error) {
	decode := tablewire.Decode
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	switch {
	case tablewire.Refused(derr):
		return loadFixRootConfigCounts{}, nil, fmt.Errorf("the engine refused the root")
	case derr != nil:
		return loadFixRootConfigCounts{}, nil, fmt.Errorf("the engine could not read the root: %w", derr)
	}
	c := loadFixRootConfigCounts{
		unknown:   rep.Unknown,
		widened:   rep.Widened,
		damaged: rep.Refused || !ok,
	}
	enc, err := tablewire.Encode(m, inst)
	if err != nil {
		return c, nil, fmt.Errorf("the engine refused to encode the value it read: %w", err)
	}
	return c, enc, nil
}

// TestLuaLoadRootConfigResaveAgreesWithTheEngine pins the root_full mutation
// that made tabledemo:RootConfig one of the wire-fuzz differential's reds.
// The mutation turns one +0 in profiles[0].ratings into -0. Both loads keep
// the value; the saves disagree because a plain float's default elision
// compares BITS, not values (docs/SPEC-TABLES.md §3.3, "A PLAIN FLOAT'S
// SPECIAL VALUES": a `[N]` array is left off only when every element's bits
// are its default's). The engine keeps the array on the re-save where a value
// comparison elides it. On the fixed form every slot rides, so the -0's bits
// are the bytes both writers owe.
func TestLuaLoadRootConfigResaveAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var paths []string
	for _, p := range units["tabledemo"] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit tabledemo: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := loadFixRootConfigProbe(ModuleName(u), "RootConfig")
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	m0 := tabletext.NewModel(u)
	def0 := m0.Lookup("RootConfig")
	if def0 == nil {
		t.Fatal("unit tabledemo declares no table RootConfig")
	}
	wire := loadFixRootConfigWire(t, m0, def0)
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
	got, saved, err := loadFixRootConfigParse(out)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("RootConfig")
	if def == nil {
		t.Fatal("unit tabledemo declares no table RootConfig")
	}
	want, encoded, err := loadFixRootConfigEngine(m, def, wire)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("the report differs: Lua says %+v, the engine says %+v", got, want)
	}
	if !bytes.Equal(saved, encoded) {
		at := 0
		for at < len(saved) && at < len(encoded) && saved[at] == encoded[at] {
			at++
		}
		t.Fatalf("the re-saved bytes differ: Lua saves %d bytes, the engine %d, first difference at byte %d\n    lua    %x\n    engine %x",
			len(saved), len(encoded), at, saved, encoded)
	}
}
