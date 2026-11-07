package luatable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

// loadFixRootConfigWire is the divergence TestLuaWireFuzzAgreesWithTheGoEngine
// reports at the unchanged base:
//
//	seed 0x13ba79ffad7d6fb8 unit tabledemo root RootConfig case root_full
//
// The third mutation writes -0.0 into profiles[0].ratings, a plain float32
// array whose every element is otherwise its zero default.
const loadFixRootConfigWire = "01010c09676f6c64656e2d7631020e360d0225030a00002242040a00007a4305040700000026062d070101080f090d070a0a0000504000000d080f0b0d070c042a00000000000d0eb8010d01b4010e0c0a706c61796572206f6e650f0e0506030102fa1008090300001102f41203d08a1305000efad5feffffff1406c81507409c16098877665544332211170b000000d700000440180e120a04000000000000000000000080000000001901011a0d511b1e1c1d0e041e021e1c1f0e051e031c002021090600000000000000220d0705040700000000230e0c0d0207030a0000803f000100240e180d020d250403000000260a0000004000072504050000000000000094d3468772c9678ab6ab7fb801d9cb41c07fb38abe08637fd2b1ac025705331732e9139e30c5314fa4edcad59a3e01a5593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9ad72f3de7edc5cd136969b1a27efe1381676743c01fe68181861b638ebaadbcc4f092206cc54cffdb69b9801f526d8babc6af9b2eef88501e750fca90918228b10334f58e3392dc5d185509fe81a9bd10a63f93a80ddb2c8ca66b6e19d19796fcf705bdbaf5278428f9b00c3d8aeb21b93c258e140a5e6fa0a3b5bb8675ce5957d48ac477299ba832b318320d7ec916c4c599f782764e0ad991c5c4aa397792c44925d7235af46fb48025f1eade1ae5c34ae5967084f5064b39e983f77f1331bf24cc8a11f5f028de419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee2600000000000000"

// loadFixRootConfigCounts is the report's six counters.
type loadFixRootConfigCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// loadFixRootConfigProbe loads one wire with the generated tolerant Load and
// re-saves it with Save, printing the counters and the saved hex.
func loadFixRootConfigProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                 clamped = 0, widened = 0, duplicate = 0 }
local v = load.Load(T, "` + root + `", inputs.wire, report)
local saved = "none"
if v ~= nil then
  local b = save.Save(T, "` + root + `", v)
  if b ~= nil then saved = hex(b) end
end
print(table.concat({ v ~= nil and 1 or 0, report.unknown, report.kindMismatch,
  report.widened, report.clamped, report.duplicate,
  tostring(report.malformed), report.refused, saved }, "|"))
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
	n := []*int{&c.unknown, &c.kindMismatch, &c.widened, &c.clamped, &c.duplicate}
	for i, p := range n {
		if _, err := fmt.Sscanf(f[1+i], "%d", p); err != nil {
			return c, nil, fmt.Errorf("probe line %q field %d: %w", line, 1+i, err)
		}
	}
	c.malformed = f[6] == "true"
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
	decode := tablewire.DecodeRegion
	if m.IsVariable(def.Name) {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			decode = tablewire.Decode
		}
	}
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
		unknown:      rep.Unknown,
		kindMismatch: rep.KindMismatch,
		widened:      rep.Widened,
		clamped:      rep.Clamped,
		duplicate:    rep.Duplicate,
		malformed:    rep.Malformed || !ok,
	}
	enc, err := tablewire.Encode(m, inst)
	if err != nil {
		return c, nil, fmt.Errorf("the engine refused to encode the value it read: %w", err)
	}
	return c, enc, nil
}

// loadFixRootConfigLeb reads a canonical LEB128 from data at at.
func loadFixRootConfigLeb(data []byte, at int) (uint64, int) {
	var v uint64
	var s uint
	for range 10 {
		x := data[at]
		at++
		v |= uint64(x&0x7f) << s
		if x&0x80 == 0 {
			return v, at
		}
		s += 7
	}
	return v, at
}

// loadFixRootConfigSkip advances past one value of the given wire kind, the
// way load.lua's skip does.
func loadFixRootConfigSkip(data []byte, at, kind int) int {
	switch kind {
	case 1, 2, 6, 20, 25:
		return at + 1
	case 3, 7, 21, 26:
		return at + 2
	case 4, 8, 10, 22, 27:
		return at + 4
	case 5, 9, 11, 23, 28, 36:
		return at + 8
	case 18, 19, 24, 29:
		return at + 16
	case 17, 30:
		_, at = loadFixRootConfigLeb(data, at)
		return at
	case 12, 13, 14, 16, 31, 32, 33:
		n, at := loadFixRootConfigLeb(data, at)
		return at + int(n)
	case 15:
		arm, at := loadFixRootConfigLeb(data, at)
		if arm != 0 {
			at++ // the arm's wire kind
			n, at := loadFixRootConfigLeb(data, at)
			return at + int(n)
		}
		return at
	}
	return at
}

// loadFixRootConfigPathAt names the root field whose header or body holds the
// byte at offset in an engine re-save.
func loadFixRootConfigPathAt(def *ir.Struct, wire []byte, offset int) string {
	if len(wire) < 9 {
		return def.Name
	}
	count := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	end := len(wire) - 8 - count*8
	if offset >= end {
		return def.Name + " (id table)"
	}
	ids := map[uint64]uint64{}
	for i := range count {
		ids[uint64(i+1)] = binary.LittleEndian.Uint64(wire[end+i*8:])
	}
	names := map[uint64]string{}
	for _, f := range def.Fields {
		names[ir.TableFieldWireId(f)] = f.Name
	}
	at := 1
	for at < end {
		start := at
		ref, next := loadFixRootConfigLeb(wire, at)
		if ref == 0 {
			break
		}
		kind := int(wire[next])
		at = min(loadFixRootConfigSkip(wire, next+1, kind), end)
		if offset >= start && offset < at {
			if name, ok := names[ids[ref]]; ok {
				return def.Name + "." + name
			}
			return def.Name
		}
	}
	return def.Name
}

// TestLuaLoadRootConfigResaveAgreesWithTheEngine pins the root_full mutation
// that made tabledemo:RootConfig one of the wire-fuzz differential's reds.
// The mutation turns one +0 in profiles[0].ratings into -0. Both loads keep
// the value; the saves disagree because a plain float's default elision
// compares BITS, not values (docs/SPEC-TABLES.md §3.3, "A PLAIN FLOAT'S
// SPECIAL VALUES": a `[N]` array is left off only when every element's bits
// are its default's). The engine keeps the array on the re-save where a value
// comparison elides it.
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
	wire, err := hex.DecodeString(loadFixRootConfigWire)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("the re-saved bytes differ: Lua saves %d bytes, the engine %d, first difference at byte %d in field %s\n    lua    %x\n    engine %x",
			len(saved), len(encoded), at, loadFixRootConfigPathAt(def, encoded, at), saved, encoded)
	}
}
