package luautable_test

// CAUSE J of the Luau wire-fuzz differential: on scalars:SimState the Go
// engine (internal/tablewire) clamps a value and counts it (clamped), and the
// Luau tolerant load does not count it. This card is the mutant of seed
// 0x0a3aef4090af8cd4 (unit scalars, root SimState, case scalars_full), rebuilt
// below; the Luau leg reads it under the pinned interpreter and the engine
// reads the same bytes, and the six counters, the verdict and the re-saved
// bytes must agree (docs/SPEC-TABLES.md §4.2).

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

type loadFixJCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixJCase struct {
	name string
	seed uint64
	unit string
	root string
	hex  string
}

// loadFixJCases is the one mutant this card fixed: seed 0x0a3aef4090af8cd4
// on scalars:SimState (case scalars_full), whose energy int128 value is past
// its declared maximum 5000000000 and clamps to it.
var loadFixJCases = []loadFixJCase{
	{
		name: "simstate int128 clamps at its declared max",
		seed: 0x0a3aef4090af8cd4, unit: "scalars", root: "SimState",
		hex: "0101148102167f004cff031700802f7500000000041800c0c0bdf0ffffffffffffffffffffff051640420f000619ef071b0000e703081cfffffeffffffffff091d00807f841e00000000000000000000000a1bffffffff0b12000000000000000000000000f0ffffff0c12fff1052a0100008000000000000000000d13ffffffffffffffffffffffffffffffff0e160080ffff0f0e0e1603008001000000f8ffffff0700100e061a028000006411101617021208000000009cffffff1308ffffffff63000000140e2213020100000000000000000000000000000000000000000000000100000000000000150d1b16170080ffffffffffff171700400c0000000000181bffff670100190d07181b000001000000c6af9b2eef88501e38861506cdb81a404ad7a1fc263abf4c58b8de05f3a11d89b5253e6a6c26d97fe4ad77811124c9fb400e20a08a498122e1d03c0919c07d8bb10a7bcea257371f6fe6b4286790e5234226af0879dd1bd6152fef40914ffdf21267e37866fdfc23911d1ab7fbb9ac6adcdd483b6acab1e31e418af06e4b49b1b8679c0d849e9632674d02864c1564afcd5002864c1764af6de14a1b3025f15a647095a80dd82f8c071702864cf563af541502864cf463af750fca90918201b1981f0eb28af728431900000000000000",
	},
}

// loadFixJProbe is the pinned interpreter's answer: the harness's 47-byte
// header (loaded, five counters, malformed, refused, measure -1, retained,
// retain_lost, saved length) and the saved bytes, lowercase hex.
const loadFixJProbe = `local S = require("./schema_tables")
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

type loadFixJAnswer struct {
	loaded, refused bool
	saveFail        bool
	counts          loadFixJCounts
	saved           []byte
}

func loadFixJParse(t *testing.T, line string) loadFixJAnswer {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil || len(raw) < 47 {
		t.Fatalf("the probe's line is not a 47-byte header in hex: %q", line)
	}
	a := loadFixJAnswer{loaded: raw[0] != 0, refused: raw[22] != 0}
	a.counts = loadFixJCounts{
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

// loadFixJEngineFor is the differential's engine oracle for one mutant: the
// report's six counters, the verdict and the re-saved bytes.
func loadFixJEngineFor(t *testing.T, units map[string][]string, unitKey, rootName string, data []byte) (loadFixJAnswer, bool) {
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
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(m, inst, data, &rep)
	var o loadFixJAnswer
	if tablewire.Refused(derr) {
		o.refused = true
		return o, true
	}
	if derr != nil {
		return o, false
	}
	o.loaded = true
	o.counts = loadFixJCounts{
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

// TestLuauLoadSimStateClampAgreesWithTheEngine is cause J's regression: the
// mutant of seed 0x0a3aef4090af8cd4 on scalars:SimState reads the same under
// the Luau tolerant load and the engine, counters, verdict and re-saved bytes
// included (docs/SPEC-TABLES.md §4.2).
func TestLuauLoadSimStateClampAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	for _, c := range loadFixJCases {
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
			b.WriteString(strings.Replace(loadFixJProbe, "ROOTNAME", strconv.Quote(c.root), 1))
			probe := filepath.Join(g.dir, "probe.luau")
			if err := os.WriteFile(probe, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("%s: mutant hex: %v", c.name, err)
			}
			line := luaurun.Run(t, g.dir, "probe.luau", map[string][]byte{"wire": wire})
			got := loadFixJParse(t, string(line))
			want, ok := loadFixJEngineFor(t, units, c.unit, c.root, wire)
			if !ok {
				t.Fatalf("seed %#016x %s: the engine refused the root itself", c.seed, c.name)
			}
			if !got.loaded || got.saveFail {
				t.Fatalf("seed %#016x %s: the Luau leg loaded=%t saveFail=%t, want a loaded, saved value (%+v)",
					c.seed, c.name, got.loaded, got.saveFail, got.counts)
			}
			if got.refused != want.refused {
				t.Fatalf("seed %#016x %s: the verdict differs: Luau refused=%t, the engine refused=%t",
					c.seed, c.name, got.refused, want.refused)
			}
			if want.counts.malformed {
				// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
				// leg owes the refusal, and no counter or saved byte is compared
				if !got.counts.malformed {
					t.Fatalf("seed %#016x %s: the engine discards a damaged load; Luau says %+v", c.seed, c.name, got.counts)
				}
				return
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
