package luautable_test

// THE MAP KEY-RULES FIX (schema-luau-fuzz-differential, cause D): read_map
// must follow the engine's map entry and key rules exactly. The seed
// 0xab0d8eb6d5b32215 on unit mapdemo, root Cells, case map_cells (mutant hex
// below) reproduces the divergence; this test rebuilds that mutant, loads it
// with the Luau tolerant load under the pinned interpreter and with the engine
// (internal/tablewire), and requires the six counters, the verdict and the
// re-saved bytes to agree.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadFixDCounts is the report's six counters, a comparable tuple.
type loadFixDCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// loadFixDAnswer is the Luau leg's answer for the mutant.
type loadFixDAnswer struct {
	raised   string
	loaded   bool
	counts   loadFixDCounts
	refused  bool
	saved    []byte
	saveFail bool
}

// loadFixDOracle is the engine's answer for the mutant.
type loadFixDOracle struct {
	noRoot  bool
	counts  loadFixDCounts
	refused bool
	encoded []byte
	encFail bool
}

// loadFixDProbe is the Luau main for one mutant: it loads tolerantly, saves,
// and prints U,K,W,C,D,m,refused,saveok,savedhex on one line.
const loadFixDProbe = `local inputs = require("./inputs")
local S = require("./schema_tables")
local load = require("./schema_load")
local save = require("./schema_save")
for _, name in inputs.modules or {} do require("./" .. name) end
local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local desc = S.lookup(inputs.root)
local v = load.loadTolerant(inputs.root, inputs.wire, report)
if v == nil and desc ~= nil and not desc.variable then v = desc.new() end
	local saved = ""
	local saveok = 0
	if v ~= nil then
		local sv = save.save(desc, v)
		if sv ~= nil then saveok = 1; saved = hex(sv) end
	end
print(string.format("%d,%d,%d,%d,%d,%s,%s,%d,%s",
	report.unknown or 0, report.kindMismatch or 0, report.widened or 0,
	report.clamped or 0, report.duplicate or 0,
	tostring(report.malformed), tostring((report.refused or 0) > 0), saveok, saved))
`

// loadFixDParseAnswer parses the probe's reply line.
func loadFixDParseAnswer(line string) loadFixDAnswer {
	var a loadFixDAnswer
	line = strings.TrimSpace(line)
	if line == "ERR" {
		a.raised = "the Luau load or save raised"
		return a
	}
	f := strings.Split(line, ",")
	if len(f) != 9 {
		a.raised = fmt.Sprintf("bad Luau reply %q", line)
		return a
	}
	fmt.Sscanf(f[0], "%d", &a.counts.unknown)
	fmt.Sscanf(f[1], "%d", &a.counts.kindMismatch)
	fmt.Sscanf(f[2], "%d", &a.counts.widened)
	fmt.Sscanf(f[3], "%d", &a.counts.clamped)
	fmt.Sscanf(f[4], "%d", &a.counts.duplicate)
	a.counts.malformed = f[5] == "true"
	a.refused = f[6] == "true"
	a.loaded = true
	a.saveFail = f[7] != "1" || f[8] == ""
	if !a.saveFail {
		a.saved, _ = hex.DecodeString(f[8])
	}
	return a
}

// loadFixDEngine reads the mutant with the compiler's engine.
func loadFixDEngine(m *tabletext.Model, def *ir.Struct, data []byte) loadFixDOracle {
	o := loadFixDOracle{}
	decode := tablewire.DecodeRegion
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	switch {
	case tablewire.Refused(derr):
		o.refused = true
	case derr != nil:
		o.noRoot = true
		return o
	default:
		o.counts = loadFixDCounts{
			unknown:      rep.Unknown,
			kindMismatch: rep.KindMismatch,
			widened:      rep.Widened,
			clamped:      rep.Clamped,
			duplicate:    rep.Duplicate,
			malformed:    rep.Malformed || !ok,
		}
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		o.encFail = true
		return o
	}
	o.encoded = enc
	return o
}

// loadFixDVerdict compares the Luau answer with the engine's and names the
// first disagreement, or "" when there is none.
func loadFixDVerdict(a loadFixDAnswer, o loadFixDOracle) string {
	if a.raised != "" {
		return "the Luau leg RAISED: " + a.raised
	}
	if o.noRoot {
		return "the engine refused the root itself"
	}
	if !a.loaded {
		return "the Luau load returned no root"
	}
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Luau refused=%t, the engine refused=%t", a.refused, o.refused)
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Luau says %+v, the engine says %+v", a.counts, o.counts)
	}
	if a.saveFail != o.encFail {
		if a.saveFail {
			return "the Luau save refused the value it loaded; the engine encoded the bytes"
		}
		return "the engine refused to encode the value it decoded; the Luau save wrote bytes"
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		return fmt.Sprintf("the decoded value differs: Luau saves %d bytes, the engine %d\n    luau   %x\n    engine %x",
			len(a.saved), len(o.encoded), a.saved, o.encoded)
	}
	return ""
}

// loadFixDRunWireFuzz runs the probe for one mutant under the pinned
// interpreter in the unit directory.
func loadFixDRunWireFuzz(t *testing.T, g *generatedUnit, rootName string, wire []byte) loadFixDAnswer {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadFixDProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("return table.freeze({\n")
	fmt.Fprintf(&b, "\troot = %q,\n", rootName)
	b.WriteString("\tmodules = {")
	for i, m := range mods {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", m)
	}
	b.WriteString("},\n\twire = buffer.fromstring(\"")
	for _, x := range wire {
		fmt.Fprintf(&b, "\\x%02x", x)
	}
	b.WriteString("\"),\n})\n")
	if err := os.WriteFile(filepath.Join(g.dir, "inputs.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	luau, why := luaurun.Interpreter()
	if luau == "" {
		t.Fatalf("luaurun: the Luau interpreter does not resolve: %s", why)
	}
	cmd := exec.Command(luau, "main.luau")
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("luau main.luau: %v\nstderr:\n%s", err, stderr.String())
	}
	return loadFixDParseAnswer(stdout.String())
}

// loadFixDCase is one regression case: the mutant of a differential seed,
// named by its seed, unit, root and case, and the rule it pins.
type loadFixDCase struct {
	what   string
	seed   uint64
	unit   string
	root   string
	kase   string
	mutant string
}

// loadFixDCases are the four differences docs/SPEC-TABLES.md §2.8 names, each
// rebuilt from the restricted differential's seed and mutant hex.
var loadFixDCases = []loadFixDCase{
	{
		what: "an entry with no key takes the key type's declared default",
		seed: 0xab0d8eb6d5b32215, unit: "mapdemo", root: "Cells", kase: "map_cells",
		mutant: "01010e370d021a020c05616c706861030e0e04030a0000000b0000000c0000000019020c0462657461030e0e0403000000000100000002000000000404070000000038810af11f06a7a3ec105b16194ac93dea0ce83094fde47ce9ea716f0f01827f0400000000000000",
	},
	{
		what: "a repeated key is a duplicate, last wins whole",
		seed: 0x15d5f996ea0c744a, unit: "listdemo", root: "Army", kase: "list_of_maps",
		mutant: "01010e580d022a020e200d020e030699040d0705041400000000000e030609040d0705045a0000000000060432000000002a020e200d020e030603040d0705041500000000000e030608040d0705045b0000000000060433000000000704080000000026a9020c9b014878424f4f300d39841cec105b36184ac93dea0ce83094fde47c74a279448ee2e5b1861b638ebaadbcc4e9ea716f0f0182bf0700000000000000",
	},
	{
		what: "a key lower than the previous one is malformed",
		seed: 0x8ef780ece722441e, unit: "listdemo", root: "Army", kase: "list_of_maps",
		mutant: "01010e580d022a020e200d020e030602040d0705041400000000000e030609040d0705045a0000000000060432000000002a020e200d020e030603040d0705041500000000000e03060804bf0705045b0000000000060433000000000704080000d50026a9020c9b014878424f4f300d39841cec105b36194ac93dea0ce83094fde47c74e279448ee2e5b1861b63e9baadbcc4e9ea716f0f0182bf0700000000000000",
	},
	{
		what: "a key past its bound drops the entry whole and counts clamped",
		seed: 0xb9936b378258f3c7, unit: "mapdemo", root: "Cells", kase: "map_cells",
		mutant: "01010e370d021a020c05616c706861030e0e04100a0000000b0000000c0000000019020c0462657461030e0e0403000000000100000002000000000404070000000038810af11f06a7a301105b36194ac93dea0ce83094fde47ce9ea716f0f0182bf0400000000000000",
	},
}

// loadFixDCheck runs one case: rebuild it with the Luau tolerant load under
// the pinned interpreter and with the engine, and require the six counters,
// the verdict and the re-saved bytes to agree.
func loadFixDCheck(t *testing.T, c loadFixDCase) {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, c.unit)
	if g.err != nil {
		t.Fatalf("unit %s does not generate: %v", c.unit, g.err)
	}
	path, err := compiler.GatherPaths(prefix(units[c.unit]))
	if err != nil {
		t.Fatalf("%s: %v", c.unit, err)
	}
	u, err := compiler.New().Load(path)
	if err != nil {
		t.Fatalf("%s does not load: %v", c.unit, err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup(c.root)
	if def == nil {
		t.Fatalf("%s declares no table %s", c.unit, c.root)
	}
	wire, err := hex.DecodeString(c.mutant)
	if err != nil {
		t.Fatalf("mutant hex: %v", err)
	}
	lu := loadFixDRunWireFuzz(t, g, c.root, wire)
	o := loadFixDEngine(m, def, wire)
	if v := loadFixDVerdict(lu, o); v != "" {
		t.Fatalf("%s: seed %#016x unit %s root %s case %s: %s\n    mutant %x", c.what, c.seed, c.unit, c.root, c.kase, v, wire)
	}
}

// TestLuauLoadMapKeyRulesAgreeWithTheEngine rebuilds one mutant per difference
// in the engine's map entry and key rules and requires the Luau tolerant load
// and the engine to agree on the six counters, the verdict and the re-saved
// bytes.
func TestLuauLoadMapKeyRulesAgreeWithTheEngine(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	for _, c := range loadFixDCases {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			loadFixDCheck(t, c)
		})
	}
}
