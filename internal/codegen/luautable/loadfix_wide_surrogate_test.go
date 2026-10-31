package luautable_test

// THE WIDE LONE SURROGATE FIX (schema-luau-fuzz-differential, cause F): a wide
// (UTF-16) text whose code units hold a lone surrogate is malformed, as the
// engine counts it, and the Luau tolerant load must count it too and keep the
// declared default (docs/SPEC-TABLES.md §3, §4). Seeds 0x8d552bf7a1fceacf on
// widedemo:Caption and 0x10512a39101c7bf4 on mapdemo:Text rebuild the two
// mutants below; each is read by the Luau tolerant load under the pinned
// interpreter and by the engine (internal/tablewire), and the six counters, the
// verdict and the re-saved bytes must agree.

import (
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

type loadFixFCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixFCase struct {
	name string
	seed uint64
	unit string
	root string
	hex  string
}

// loadFixFCases carries the two roots cause F names.
var loadFixFCases = []loadFixFCase{
	{
		// seed 0x8d552bf7a1fceacf, unit widedemo, root Caption (case
		// wide_sites): a high surrogate not followed by a low one.
		name: "widedemo caption high surrogate alone",
		seed: 0x8d552bf7a1fceacf, unit: "widedemo", root: "Caption",
		hex: "0101210841003d56a9dc4200020d080321044100420000040e160d020803210441004200000a0321063c043804400400050f062104410042000029601b0c6c2931da07594f69ada54bbf7e409519eff404fa1c00d5f1a9f9e35c9532c9c69be74dcdca5c7155f6f133a60600000000000000",
	},
	{
		// seed 0x10512a39101c7bf4, unit mapdemo, root Text (case map_text): a
		// low surrogate with no high before it.
		name: "mapdemo text low surrogate first",
		seed: 0x10512a39101c7bf4, unit: "mapdemo", root: "Text",
		hex: "01010e260d0211020c05616c706861030c0566697273740011020c0462657461030c067365636f6e6400040e150d01120207050803210a4100e9002d4e3dff00de00050e240d02100204fdffffff030e060604deadbeef0010020402000000030e060604000180ff00060409000000004f8168f2ff28b7afec105b36194ac93dea0ce83094fde47cca5c7155f6f133a65b92de9cabeae214e9ea716f0f0182bf0600000000000000",
	},
}

// loadFixFProbe is the pinned interpreter's answer: the harness's 47-byte
// header (loaded, five counters, malformed, refused, measure -1, retained,
// retain_lost, saved length) and the saved bytes, lowercase hex.
const loadFixFProbe = `local S = require("./schema_tables")
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

type loadFixFAnswer struct {
	loaded, refused bool
	saveFail        bool
	counts          loadFixFCounts
	saved           []byte
}

func loadFixFParse(t *testing.T, line string) loadFixFAnswer {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil || len(raw) < 47 {
		t.Fatalf("the probe's line is not a 47-byte header in hex: %q", line)
	}
	a := loadFixFAnswer{loaded: raw[0] != 0, refused: raw[22] != 0}
	a.counts = loadFixFCounts{
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

func loadFixFEngineFor(t *testing.T, units map[string][]string, unitKey, rootName string, data []byte) (loadFixFAnswer, bool) {
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
	var o loadFixFAnswer
	if tablewire.Refused(derr) {
		o.refused = true
		return o, true
	}
	if derr != nil {
		return o, false
	}
	o.loaded = true
	o.counts = loadFixFCounts{
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

func TestLuauLoadWideLoneSurrogateAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	for _, c := range loadFixFCases {
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
			b.WriteString(strings.Replace(loadFixFProbe, "ROOTNAME", strconv.Quote(c.root), 1))
			probe := filepath.Join(g.dir, "probe.luau")
			if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("%s: mutant hex: %v", c.name, err)
			}
			line := luaurun.Run(t, g.dir, "probe.luau", map[string][]byte{"wire": wire})
			got := loadFixFParse(t, string(line))
			want, ok := loadFixFEngineFor(t, units, c.unit, c.root, wire)
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
			if string(got.saved) != string(want.saved) {
				t.Fatalf("seed %#016x %s: the decoded value differs: Luau saves %x, the engine %x",
					c.seed, c.name, got.saved, want.saved)
			}
		})
	}
}
