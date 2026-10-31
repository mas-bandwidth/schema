package luautable_test

// CAUSE E of the Luau wire-fuzz differential: the tolerant read's enum-keyed
// arrays and the save's blob numbering. Each case is one mutant the
// differential reported, rebuilt from its seed; the Luau leg reads it under
// the pinned interpreter and the engine (internal/tablewire) reads the same
// bytes, and the six counters, the verdict and the re-saved bytes must agree
// (docs/SPEC-TABLES.md §2.5, §3.2, §4.2).

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

type loadFixECounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixECase struct {
	name string
	seed uint64
	unit string
	root string
	hex  string
	// refuseSave: the loaded value names one blob from two slots, so both
	// the engine and the Luau save refuse it (docs/SPEC-TABLES.md §2.5)
	refuseSave bool
}

// loadFixECases carries one case per group this card fixed.
var loadFixECases = []loadFixECase{
	{
		// keyed_config: an enum-keyed element body that did not spend its L
		// kept its partial value, so the save wrote fields the engine had
		// reset to their declared defaults (docs/SPEC-TABLES.md §3.2, §5).
		name: "keyed element body falls back to defaults",
		seed: 0x03fcd054291f650e, unit: "tabledemo", root: "KeyedConfig",
		hex: "010110220d02020d030408000000040c0372656400050f03e902000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a830020420d0d0100000e1a0c0a0000a0400f0a00001041820d0a100a0000403f1101010000001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0051c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
	},
	{
		// keyed_config: a nested optional body inside a keyed element body that
		// did not spend its L; the engine refills it, and the slot names the
		// declared default rather than the partial read (docs/SPEC-TABLES.md
		// §3, §5).
		name: "nested optional body falls back to defaults",
		seed: 0xc1dec918be963b9f, unit: "tabledemo", root: "KeyedConfig",
		hex: "010110220d02020d030408000000040c0372656400050f030402000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101019700001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0c01c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bc3002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000",
	},
	{
		// blob_shared: the tolerant load keeps one blob named from two slots,
		// and every writer refuses that value by name, so the engine and the
		// Luau save both refuse to re-save it (docs/SPEC-TABLES.md §2.5).
		name: "shared blob refuses to re-save", refuseSave: true,
		seed: 0xf5e118f73bb8f76d, unit: "blobdemo", root: "Catalog",
		hex: "01010c047477696e021102031102040c070105041020304000861b638eba01bcc403b2f40f72193b61b746a665bb209250ffffffffffffffffe44f1c4f47c02e2f0500000000000000",
	},
}

// loadFixEProbe is the pinned interpreter's answer: the harness's 47-byte
// header (loaded, five counters, malformed, refused, measure -1, retained,
// retain_lost, saved length) and the saved bytes, lowercase hex.
const loadFixEProbe = `local S = require("./schema_tables")
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

type loadFixEAnswer struct {
	loaded, refused bool
	saveFail        bool
	counts          loadFixECounts
	saved           []byte
}

func loadFixEParse(t *testing.T, line string) loadFixEAnswer {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil || len(raw) < 47 {
		t.Fatalf("the probe's line is not a 47-byte header in hex: %q", line)
	}
	a := loadFixEAnswer{loaded: raw[0] != 0, refused: raw[22] != 0}
	a.counts = loadFixECounts{
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

// loadFixEEngineFor is the differential's engine oracle for one mutant: a
// variable root's region is measured first, and a framing refusal falls back
// to the plain Decode, exactly as the differential does.
func loadFixEEngineFor(t *testing.T, units map[string][]string, unitKey, rootName string, data []byte) (loadFixEAnswer, bool) {
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
	var o loadFixEAnswer
	if tablewire.Refused(derr) {
		o.refused = true
		return o, true
	}
	if derr != nil {
		return o, false
	}
	o.loaded = true
	o.counts = loadFixECounts{
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

func TestLuauLoadKeyedAndBlobSaveAgreeWithTheEngine(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	for _, c := range loadFixECases {
		t.Run(c.name, func(t *testing.T) {
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
			b.WriteString(strings.Replace(loadFixEProbe, "ROOTNAME", strconv.Quote(c.root), 1))
			probe := filepath.Join(g.dir, "probe.luau")
			if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("%s: mutant hex: %v", c.name, err)
			}
			line := luaurun.Run(t, g.dir, "probe.luau", map[string][]byte{"wire": wire})
			got := loadFixEParse(t, string(line))
			want, ok := loadFixEEngineFor(t, units, c.unit, c.root, wire)
			if !ok {
				t.Fatalf("seed %#016x %s: the engine refused the root itself", c.seed, c.name)
			}
			if c.refuseSave {
				if !got.loaded || !got.saveFail || !want.saveFail {
					t.Fatalf("seed %#016x %s: loaded=%t, Luau saveFail=%t, engine saveFail=%t; want a loaded value both refuse to save",
						c.seed, c.name, got.loaded, got.saveFail, want.saveFail)
				}
				if got.counts != want.counts {
					t.Fatalf("seed %#016x %s: the report differs: Luau says %+v, the engine says %+v",
						c.seed, c.name, got.counts, want.counts)
				}
				return
			}
			if !got.loaded || got.saveFail {
				t.Fatalf("seed %#016x %s: the Luau leg loaded=%t saveFail=%t, want a loaded, saved value (%+v)",
					c.seed, c.name, got.loaded, got.saveFail, got.counts)
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
			if string(got.saved) != string(want.saved) {
				t.Fatalf("seed %#016x %s: the decoded value differs: Luau saves %x, the engine %x",
					c.seed, c.name, got.saved, want.saved)
			}
		})
	}
}
