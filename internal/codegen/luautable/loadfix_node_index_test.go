package luautable_test

import (
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadFixBSeed is the differential's mutant seed. The wire below is that
// mutant of unit graphdemo, root Scene (case graph_tree): a node record whose
// type id this reader cannot place sits between two records a pointer names,
// and on the unfixed runtime the skipped record is written as nil, which does
// not grow the Luau array, so every later index shifts down by one.
const loadFixBSeed = "0x454c33200fe28804"

// loadFixBWireHex is the mutant's bytes, verbatim (docs/SPEC-TABLES.md §3).
const loadFixBWireHex = "01010c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c79f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a0e9d66fe00649160807a0f00000000000000"

// loadFixBAnswer is one leg's answer: the loaded flag, the six counters, the
// refused verdict, and the re-saved bytes (docs/SPEC-TABLES.md §4).
type loadFixBAnswer struct {
	loaded       bool
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	duplicate    int
	malformed    bool
	refused      bool
	saveFail     bool
	saved        []byte
}

func (a loadFixBAnswer) counts() string {
	return fmt.Sprintf("unknown=%d kindMismatch=%d widened=%d clamped=%d duplicate=%d malformed=%t",
		a.unknown, a.kindMismatch, a.widened, a.clamped, a.duplicate, a.malformed)
}

// loadFixBEngine reads the wire with the compiler's engine exactly as the
// differential's oracle does for a file root
// (internal/codegen/luautable/fuzz_test.go, luauFuzzEngine).
func loadFixBEngine(t *testing.T, m *tabletext.Model, def *ir.Struct, data []byte) loadFixBAnswer {
	t.Helper()
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
		return loadFixBAnswer{refused: true, saveFail: true}
	}
	if derr != nil {
		t.Fatalf("seed %s: the engine refused the root itself: %v", loadFixBSeed, derr)
	}
	a := loadFixBAnswer{
		loaded:       true,
		unknown:      rep.Unknown,
		kindMismatch: rep.KindMismatch,
		widened:      rep.Widened,
		clamped:      rep.Clamped,
		duplicate:    rep.Duplicate,
		malformed:    rep.Malformed || !ok,
		refused:      rep.Refused,
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		a.saveFail = true
		return a
	}
	a.saved = enc
	return a
}

// loadFixBMain is the Luau probe: it loads the mutant through the emitted
// tolerant load, prints the loaded flag, the six counters and the verdict,
// and re-saves the loaded value with the emitted save
// (docs/SPEC-TABLES.md §4, §6.3).
func loadFixBMain(g *generatedUnit) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local S = require(\"./schema_tables\")\n")
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
	b.WriteString(`local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local d = S.lookup("Scene")
local v = load.loadTolerant("Scene", inputs.wire, report)
local saved = nil
if v ~= nil then saved = save.save(d, v) end
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local savedText = "none"
if saved ~= nil then savedText = hex(saved) end
print(table.concat({
	tostring(v ~= nil),
	tostring(report.unknown),
	tostring(report.kindMismatch),
	tostring(report.widened),
	tostring(report.clamped),
	tostring(report.duplicate),
	tostring(report.malformed),
	tostring((report.refused or 0) > 0),
	savedText,
}, "|"))
`)
	return b.String()
}

// loadFixBParse parses the probe's one line.
func loadFixBParse(t *testing.T, line string) loadFixBAnswer {
	t.Helper()
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 9 {
		t.Fatalf("seed %s: the Luau probe printed %d fields, want 9: %q", loadFixBSeed, len(f), line)
	}
	atoi := func(s string) int {
		v, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("seed %s: probe field %q is not an integer: %v", loadFixBSeed, s, err)
		}
		return v
	}
	a := loadFixBAnswer{
		loaded:       f[0] == "true",
		unknown:      atoi(f[1]),
		kindMismatch: atoi(f[2]),
		widened:      atoi(f[3]),
		clamped:      atoi(f[4]),
		duplicate:    atoi(f[5]),
		malformed:    f[6] == "true",
		refused:      f[7] == "true",
	}
	if f[8] != "none" {
		b, err := hex.DecodeString(f[8])
		if err != nil {
			t.Fatalf("seed %s: saved bytes are not hex: %v", loadFixBSeed, err)
		}
		a.saved = b
	} else {
		a.saveFail = true
	}
	return a
}

// TestLuauLoadNodeIndexesDoNotShiftAgreesWithTheEngine pins docs/SPEC-TABLES.md
// §3.1's numbering: a node record this reader cannot place KEEPS ITS INDEX, and
// every pointer naming it reads null, counted once at the node and never once
// per pointer. The Luau load records a placeholder for it instead of nil (which
// would not grow the array and shift every later index down), and resolve()
// treats the placeholder as absent. The mutant of seed 0x454c33200fe28804,
// unit graphdemo, root Scene is that graph, and the runtime must agree with the
// engine on the six counters, the verdict and the re-saved bytes.
func TestLuauLoadNodeIndexesDoNotShiftAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	wire, err := hex.DecodeString(loadFixBWireHex)
	if err != nil {
		t.Fatalf("seed %s: the mutant literal is not hex: %v", loadFixBSeed, err)
	}

	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatalf("unit graphdemo: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit graphdemo does not load: %v", err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("Scene")
	if def == nil {
		t.Fatal("unit graphdemo declares no table Scene")
	}
	want := loadFixBEngine(t, m, def, wire)

	g := generateUnit(t, units, "graphdemo")
	if !g.loaded {
		t.Fatalf("the Luau target refuses unit graphdemo: %v", g.err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadFixBMain(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))
	got := loadFixBParse(t, out)

	if got.loaded != want.loaded {
		t.Errorf("seed %s: loaded differs: Luau %t, the engine %t", loadFixBSeed, got.loaded, want.loaded)
	}
	if got.counts() != want.counts() {
		t.Errorf("seed %s: the report differs: Luau says {%s}, the engine says {%s}", loadFixBSeed, got.counts(), want.counts())
	}
	if got.refused != want.refused {
		t.Errorf("seed %s: the verdict differs: Luau refused=%t, the engine refused=%t", loadFixBSeed, got.refused, want.refused)
	}
	if got.saveFail != want.saveFail {
		t.Errorf("seed %s: save refusal differs: Luau saveFail=%t, the engine saveFail=%t", loadFixBSeed, got.saveFail, want.saveFail)
	} else if !got.saveFail && string(got.saved) != string(want.saved) {
		t.Errorf("seed %s: the re-saved bytes differ: Luau saves %d bytes, the engine %d\nluau   %x\nengine %x",
			loadFixBSeed, len(got.saved), len(want.saved), got.saved, want.saved)
	}
}
