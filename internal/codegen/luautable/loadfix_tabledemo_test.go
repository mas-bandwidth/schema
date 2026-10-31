package luautable_test

// CAUSE H of the Luau wire-fuzz differential (docs/SPEC-TABLES.md §3, §3.2,
// §4):
//
// 1. A bool wire byte (kind 1) past 1 is malformed: the engine counts
//    malformed when b > 1 and still sets the bool to b != 0 (internal/tablewire
//    decode.go scalarAt). Seeds 0xc7397fb3a839bcf5 and 0x3e01a79eb216759f on
//    tabledemo:ProfileConfig and seed 0xf61eafb41ebe6a4e on tabledemo:KeyedConfig
//    carry bool bytes past 1 (0x41, 0x52, 0xff) and missed the malformed count.
//
// 2. A payload-free union arm (void) carries L = 0; an L that is not zero is
//    that arm's own framing damage: the union reads None and malformed counts
//    (docs/SPEC-TABLES.md §3, internal/tablewire decode.go arm). Seed
//    0xeb873532b5ef5474 on tblr1:Cfg gives the void arm ping L = 16, consuming
//    subsequent root fields as the arm's payload and leaving the arm selected
//    instead of None.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type loadFixHCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixHCase struct {
	name string
	seed uint64
	unit string
	root string
	hex  string
}

var loadFixHCases = []loadFixHCase{
	{
		// seed 0xc7397fb3a839bcf5, unit tabledemo, root ProfileConfig (case
		// profile_elide): bool byte 0x41 (> 1) on field 2 (has_loadout).
		name: "tabledemo ProfileConfig bool byte past 1",
		seed: 0xc7397fb3a839bcf5, unit: "tabledemo", root: "ProfileConfig",
		hex: "01010801000000020141030d0a040e060d0201000178000069b9801f526d8bab3c258e140a5e6fa0e9b5bb8675ce595724cc8a11f5ee28de0400000000000000",
	},
	{
		// seed 0xf61eafb41ebe6a4e, unit tabledemo, root KeyedConfig (case
		// keyed_config): bool byte 0xff (> 1) on GunnerConfig.tracking inside
		// a keyed HullConfig.turrets slot.
		name: "tabledemo KeyedConfig nested bool byte past 1",
		seed: 0xf61eafb41ebe6a4e, unit: "tabledemo", root: "KeyedConfig",
		hex: "010110220d02020d030408000000040c0372656400050f030402000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101ff0000001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
	},
	{
		// seed 0xeb873532b5ef5474, unit tblr1, root Cfg (case r1_ping): void
		// union arm ping carries L = 16 instead of 0.
		name: "tblr1 Cfg void union arm nonzero length",
		seed: 0xeb873532b5ef5474, unit: "tblr1", root: "Cfg",
		hex: "01011e02030f042010050d07060a0000c03f00070e041e020208091008040102040708e50000d48ac477299ba8328025f1eade1ae5c39443f2513bc8c136a90733c50de030bfcc69e4e29bbeb5ffc6875c803a62dc9ac599f782764e0ad9b318320d7ec916c4dd542dd0294d3faa0900000000000000",
	},
}

const loadFixHProbe = `local S = require("./schema_tables")
local load = require("./schema_load")
local save = require("./schema_save")
local inputs = require("./inputs")

local root = ROOTNAME

local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local report = {
	malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
	clamped = 0, widened = 0, duplicate = 0,
}
local desc = S.lookup(root)
local v = load.loadTolerant(root, inputs.wire, report)
if v == nil and desc ~= nil and not desc.variable then v = desc.new() end
local saved = nil
if v ~= nil then saved = save.save(desc, v) end
local n = 47 + (if saved ~= nil then buffer.len(saved) else 0)
local b = buffer.create(n)
buffer.writeu8(b, 0, if v ~= nil then 1 else 0)
buffer.writei32(b, 1, report.unknown or 0)
buffer.writei32(b, 5, report.kindMismatch or 0)
buffer.writei32(b, 9, report.widened or 0)
buffer.writei32(b, 13, report.clamped or 0)
buffer.writei32(b, 17, report.duplicate or 0)
buffer.writeu8(b, 21, if report.malformed then 1 else 0)
buffer.writeu8(b, 22, if (report.refused or 0) > 0 then 1 else 0)
buffer.writeinteger(b, 23, -1i)
buffer.writei32(b, 31, 0)
buffer.writei32(b, 35, 0)
if saved == nil then
	buffer.writeinteger(b, 39, -1i)
else
	buffer.writeinteger(b, 39, integer.create(buffer.len(saved)))
	buffer.copy(b, 47, saved, 0, buffer.len(saved))
end
print(hex(b))
`

type loadFixHAnswer struct {
	loaded, refused bool
	saveFail        bool
	counts          loadFixHCounts
	saved           []byte
}

func loadFixHParse(t *testing.T, line string) loadFixHAnswer {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil || len(raw) < 47 {
		t.Fatalf("the probe's line is not a 47-byte header in hex: %q", line)
	}
	a := loadFixHAnswer{loaded: raw[0] != 0, refused: raw[22] != 0}
	a.counts = loadFixHCounts{
		unknown:      int(int32(binary.LittleEndian.Uint32(raw[1:]))),
		kindMismatch: int(int32(binary.LittleEndian.Uint32(raw[5:]))),
		widened:      int(int32(binary.LittleEndian.Uint32(raw[9:]))),
		clamped:      int(int32(binary.LittleEndian.Uint32(raw[13:]))),
		duplicate:    int(int32(binary.LittleEndian.Uint32(raw[17:]))),
		malformed:    raw[21] != 0,
	}
	n := int64(binary.LittleEndian.Uint64(raw[39:]))
	if n < 0 {
		a.saveFail = true
		return a
	}
	if int64(len(raw)) != 47+n {
		t.Fatalf("the probe's header names %d saved bytes, the line carries %d", n, len(raw)-47)
	}
	a.saved = raw[47:]
	return a
}

func loadFixHEngineFor(t *testing.T, units map[string][]string, unitKey, rootName string, data []byte) (loadFixHAnswer, bool) {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix(units[unitKey]))
	if err != nil {
		t.Fatalf("unit %s: %v", unitKey, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", unitKey, err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup(rootName)
	if def == nil {
		t.Fatalf("unit %s declares no table %s", unitKey, rootName)
	}
	decode := tablewire.DecodeRegion
	if m.IsVariable(def.Name) {
		if _, _, refused := tablewire.FileRegionMeasure(u, def, data); refused {
			decode = tablewire.Decode
		}
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	var o loadFixHAnswer
	if tablewire.Refused(derr) {
		o.refused = true
		return o, true
	}
	if derr != nil {
		return o, false
	}
	o.loaded = true
	o.counts = loadFixHCounts{
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
		return o, true
	}
	o.saved = enc
	return o, true
}

func TestLuauLoadTabledemoMalformedAndResaveAgreeWithTheEngine(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	for _, c := range loadFixHCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := generateUnit(t, units, c.unit)
			if g.err != nil {
				t.Fatalf("%s: unit %s does not generate: %v", c.name, c.unit, g.err)
			}
			var mods []string
			for name := range g.files {
				if strings.HasSuffix(name, "Table.luau") {
					mods = append(mods, strings.TrimSuffix(name, ".luau"))
				}
			}
			sort.Strings(mods)
			var b strings.Builder
			for _, m := range mods {
				fmt.Fprintf(&b, "require(\"./%s\")\n", m)
			}
			b.WriteString(strings.Replace(loadFixHProbe, "ROOTNAME", strconv.Quote(c.root), 1))
			probe := filepath.Join(g.dir, "probe.luau")
			if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("%s: mutant hex: %v", c.name, err)
			}
			line := luaurun.Run(t, g.dir, "probe.luau", map[string][]byte{"wire": wire})
			got := loadFixHParse(t, string(line))
			want, ok := loadFixHEngineFor(t, units, c.unit, c.root, wire)
			if !ok {
				t.Fatalf("seed %#016x %s: the engine refused the root itself", c.seed, c.name)
			}
			if got.refused != want.refused {
				t.Fatalf("seed %#016x %s: the verdict differs: Luau refused=%t, the engine refused=%t",
					c.seed, c.name, got.refused, want.refused)
			}
			if got.counts != want.counts {
				t.Fatalf("seed %#016x %s: the report differs: Luau says %+v, the engine says %+v",
					c.seed, c.name, got.counts, want.counts)
			}
			if want.saveFail {
				t.Fatalf("seed %#016x %s: the engine refused to encode the value it decoded", c.seed, c.name)
			}
			if got.saveFail {
				t.Fatalf("seed %#016x %s: the Luau save refused the value it loaded", c.seed, c.name)
			}
			if !bytes.Equal(got.saved, want.saved) {
				t.Fatalf("seed %#016x %s: the decoded value differs: Luau saves %x, the engine %x",
					c.seed, c.name, got.saved, want.saved)
			}
		})
	}
}
