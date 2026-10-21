package luatable

// THE TABLEDEMO MALFORMED CAUSE of the Lua wire-fuzz differential
// (docs/SPEC-TABLES.md §3, §4): a bool wire byte (kind 1) past 1 is malformed,
// and the bool is still true when the byte is not zero (internal/tablewire
// decode.go scalarAt). Each case is one mutant of
// SCHEMA_LUA_FUZZ_DIVERGENCES that the Lua tolerant load reported
// malformed:false where the Go engine reported malformed:true.

import (
	"bytes"
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

type loadFixTabledemoCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixTabledemoAnswer struct {
	loaded, refused, saveFail bool
	counts                    loadFixTabledemoCounts
	saved                     []byte
}

type loadFixTabledemoCase struct {
	name string
	seed uint64
	unit string
	root string
	hex  string
}

var loadFixTabledemoCases = []loadFixTabledemoCase{
	{
		// seed 0xc7397fb3a839bcf5, unit tabledemo, root ProfileConfig (case
		// profile_elide): bool byte 0x41 (> 1) on field 2 (has_loadout).
		name: "bool byte past 1 in ProfileConfig",
		seed: 0xc7397fb3a839bcf5, unit: "tabledemo", root: "ProfileConfig",
		hex: "01010801000000020141030d0a040e060d0201000178000069b9801f526d8bab3c258e140a5e6fa0e9b5bb8675ce595724cc8a11f5ee28de0400000000000000",
	},
	{
		// seed 0x3e01a79eb216759f, unit tabledemo, root ProfileConfig (case
		// profile_elide): bool byte 0x52 (> 1) on field 2 (has_loadout).
		name: "bool byte 0x52 past 1 in ProfileConfig",
		seed: 0x3e01a79eb216759f, unit: "tabledemo", root: "ProfileConfig",
		hex: "01010801000000020152030d0a040e060d0201000100000069b9801f526d8bab3c258e140a5e6fa0a3b5bb8675ce595724738a11f5f028de0400000000000000",
	},
	{
		// seed 0xf61eafb41ebe6a4e, unit tabledemo, root KeyedConfig (case
		// keyed_config): bool byte 0xff (> 1) on GunnerConfig.tracking inside
		// a keyed HullConfig.turrets slot.
		name: "bool byte 0xff past 1 in KeyedConfig",
		seed: 0xf61eafb41ebe6a4e, unit: "tabledemo", root: "KeyedConfig",
		hex: "010110220d02020d030408000000040c0372656400050f030402000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101ff0000001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
	},
}

const loadFixTabledemoCost = "the pinned Lua interpreter (dist/lua-5.4.8, make dist-lua) over the tabledemo malformed mutants"

func loadFixTabledemoProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local wire = inputs.wire
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                 clamped = 0, widened = 0, duplicate = 0 }
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local v = load.LoadTolerant(T, "` + root + `", wire, report)
local loaded = 0
local savedHex = "none"
if v ~= nil then
  loaded = 1
  local b = save.Save(T, "` + root + `", v)
  if b ~= nil then savedHex = hex(b) end
end
print(table.concat({ loaded, report.unknown, report.kindMismatch,
  report.widened, report.clamped, report.duplicate,
  tostring(report.malformed), tostring(report.refused), savedHex }, "|"))
`
}

func loadFixTabledemoParse(t *testing.T, line string) loadFixTabledemoAnswer {
	t.Helper()
	var a loadFixTabledemoAnswer
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 9 {
		t.Fatalf("the probe line has %d fields, want 9: %q", len(f), line)
	}
	a.loaded = f[0] == "1"
	n := []*int{&a.counts.unknown, &a.counts.kindMismatch, &a.counts.widened, &a.counts.clamped, &a.counts.duplicate}
	for i, p := range n {
		if _, err := fmt.Sscanf(f[1+i], "%d", p); err != nil {
			t.Fatalf("the probe line %q field %d: %v", line, 1+i, err)
		}
	}
	a.counts.malformed = f[6] == "true"
	a.refused = f[7] != "0"
	if f[8] == "none" {
		a.saveFail = true
		return a
	}
	saved, err := hex.DecodeString(f[8])
	if err != nil {
		t.Fatalf("the probe line %q saved bytes: %v", line, err)
	}
	a.saved = saved
	return a
}

func loadFixTabledemoEngine(t *testing.T, m *tabletext.Model, def *ir.Struct, data []byte) loadFixTabledemoAnswer {
	t.Helper()
	var o loadFixTabledemoAnswer
	decode := tablewire.DecodeRegion
	if m.IsVariable(def.Name) {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			decode = tablewire.Decode
		}
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	if tablewire.Refused(derr) {
		o.refused = true
		return o
	}
	if derr != nil {
		t.Fatalf("the engine refused the root itself: %v", derr)
	}
	o.loaded = true
	o.counts = loadFixTabledemoCounts{
		unknown:      rep.Unknown,
		kindMismatch: rep.KindMismatch,
		widened:      rep.Widened,
		clamped:      rep.Clamped,
		duplicate:    rep.Duplicate,
		malformed:    rep.Malformed || !ok,
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		o.saveFail = true
		return o
	}
	o.saved = enc
	return o
}

// TestLuaLoadTabledemoMalformedAgreesWithTheEngine pins one mutant per seed of
// the tabledemo malformed divergence: the Lua tolerant load and the Go engine
// must agree on the six counters, the verdict and the re-saved bytes
// (docs/SPEC-TABLES.md §3, §4).
func TestLuaLoadTabledemoMalformedAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, loadFixTabledemoCost)
	repo := filepath.Join("..", "..", "..")
	units := manifestUnits(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	for _, c := range loadFixTabledemoCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			paths, ok := units[c.unit]
			if !ok {
				t.Fatalf("the manifest declares no unit %s", c.unit)
			}
			var full []string
			for _, p := range paths {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", c.unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(loadFixTabledemoProbe(ModuleName(u), c.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			data, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("seed %#016x %s: mutant hex: %v", c.seed, c.name, err)
			}
			out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": data})), "\n")
			got := loadFixTabledemoParse(t, out)
			m := tabletext.NewModel(u)
			def := m.Lookup(c.root)
			if def == nil {
				t.Fatalf("unit %s declares no table %s", c.unit, c.root)
			}
			want := loadFixTabledemoEngine(t, m, def, data)
			if got.refused != want.refused {
				t.Fatalf("seed %#016x %s: the verdict differs: Lua refused=%t, the engine refused=%t",
					c.seed, c.name, got.refused, want.refused)
			}
			if got.counts != want.counts {
				t.Fatalf("seed %#016x %s: the report differs: Lua says %+v, the engine says %+v",
					c.seed, c.name, got.counts, want.counts)
			}
			if got.saveFail != want.saveFail {
				t.Fatalf("seed %#016x %s: the save verdict differs: Lua saveFail=%t, the engine saveFail=%t",
					c.seed, c.name, got.saveFail, want.saveFail)
			}
			if !got.saveFail && !bytes.Equal(got.saved, want.saved) {
				t.Fatalf("seed %#016x %s: the decoded value differs: Lua saves %x, the engine %x",
					c.seed, c.name, got.saved, want.saved)
			}
		})
	}
}
