package luautable_test

// THE EXTRA KIND-MISMATCH FIX (schema-luau-fuzz-differential, cause I): the
// Luau tolerant load counted a kind_mismatch the engine does not count when a
// node table failed to read whole. The seeds 0x0db38a87c26d573c on tblg1:
// Guarded (case guard_true_both_edges) and 0x70aef99c722b66e1 on mapdemo:
// Pairs (case map_pairs) reproduce it (each mutant hex below): a pointer to
// the root under a numbering that failed counted `resolve-root` while the
// engine resolves nothing against a failed numbering (§3.1, "No index ever
// resolves against a numbering that failed"). This test rebuilds each mutant,
// loads it with the Luau tolerant load under the pinned interpreter and with
// the engine (internal/tablewire), and requires the six counters, the verdict
// and the re-saved bytes to agree.

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

// loadFixICounts is the report's six counters, a comparable tuple.
type loadFixICounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// loadFixIAnswer is the Luau leg's answer for one mutant.
type loadFixIAnswer struct {
	raised   string
	loaded   bool
	counts   loadFixICounts
	refused  bool
	saved    []byte
	saveFail bool
}

// loadFixIOracle is the engine's answer for one mutant.
type loadFixIOracle struct {
	variable       bool
	framingRefused bool
	noRoot         bool
	counts         loadFixICounts
	refused        bool
	encoded        []byte
	encFail        bool
}

// loadFixIProbe is the Luau main for one mutant: it loads tolerantly, saves,
// and prints U,K,W,C,D,m,refused,saveok,savedhex on one line.
const loadFixIProbe = `local inputs = require("./inputs")
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

// loadFixIParseAnswer parses the probe's reply line.
func loadFixIParseAnswer(line string) loadFixIAnswer {
	var a loadFixIAnswer
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

// loadFixIEngine reads one mutant with the compiler's engine, mirroring the
// differential's oracle (fuzz_test.go, luauFuzzEngine): a variable root's
// region is measured first, and a wire the framing refuses is read by the
// plain Decode.
func loadFixIEngine(m *tabletext.Model, def *ir.Struct, data []byte) loadFixIOracle {
	o := loadFixIOracle{variable: m.IsVariable(def.Name)}
	decode := tablewire.DecodeRegion
	if o.variable {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			o.framingRefused = true
			decode = tablewire.Decode
		}
	}
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
		o.counts = loadFixICounts{
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

// loadFixIVerdict compares the Luau answer with the engine's and names the
// first disagreement, or "" when there is none.
func loadFixIVerdict(a loadFixIAnswer, o loadFixIOracle) string {
	if a.raised != "" {
		return "the Luau leg RAISED: " + a.raised
	}
	if o.noRoot {
		return "the engine refused the root itself"
	}
	if !a.loaded {
		if !o.variable {
			return "the Luau load returned no root for a FIXED root, which always loads"
		}
		if a.refused != o.refused {
			return fmt.Sprintf("the Luau load returned no root and the verdict differs: Luau refused=%t, the engine refused=%t", a.refused, o.refused)
		}
		if o.refused || o.framingRefused || o.counts.malformed {
			return ""
		}
		return fmt.Sprintf("the Luau load returned no root; the engine read a value (%+v)", o.counts)
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

// loadFixIRunWireFuzz runs the probe for one mutant under the pinned
// interpreter in the unit directory.
func loadFixIRunWireFuzz(t *testing.T, g *generatedUnit, rootName string, wire []byte) loadFixIAnswer {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadFixIProbe), 0o644); err != nil {
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
	return loadFixIParseAnswer(stdout.String())
}

// loadFixIKindMismatchCase is one regression case: the mutant of a differential
// seed, named by its seed, unit, root and case.
type loadFixIKindMismatchCase struct {
	what   string
	seed   uint64
	unit   string
	root   string
	kase   string
	mutant string
}

// loadFixIKindMismatchCases are the two divergences of cause I, each rebuilt
// from the restricted differential's seed and mutant hex.
var loadFixIKindMismatchCases = []loadFixIKindMismatchCase{
	{
		what: "a pointer to the root under a failed numbering counts no kind mismatch",
		seed: 0x0db38a87c26d573c, unit: "tblg1", root: "Guarded", kase: "guard_true_both_edges",
		mutant: "01010101021102031101040c130205270604010000000005070604020000000000c740a70b98ed73d744e5f7fadfa6c4f7c15dd12f298f25e3ffffffffffffffff8db6f6d2c61cbd66ea0ce83094fde47c0600000000000000",
	},
	{
		what: "a map entry pointer under a failed numbering counts no kind mismatch",
		seed: 0x70aef99c722b66e1, unit: "mapdemo", root: "Pairs", kase: "map_pairs",
		mutant: "01010e200d020e020804000000030e0411020201000e020809000000030e041102030094040405000000054c1302060707042800000000060707045a00000000004656eeb16b2e8ce6ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf5274a279448ee2e5b10700000000000000",
	},
}

// loadFixICheck runs one case: rebuild it with the Luau tolerant load under
// the pinned interpreter and with the engine, and require the six counters,
// the verdict and the re-saved bytes to agree.
func loadFixICheck(t *testing.T, c loadFixIKindMismatchCase) {
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
	lu := loadFixIRunWireFuzz(t, g, c.root, wire)
	o := loadFixIEngine(m, def, wire)
	if v := loadFixIVerdict(lu, o); v != "" {
		t.Fatalf("%s: seed %#016x unit %s root %s case %s: %s\n    mutant %x", c.what, c.seed, c.unit, c.root, c.kase, v, wire)
	}
}

// TestLuauLoadKindMismatchAgreesWithTheEngine rebuilds the two mutants of
// cause I and requires the Luau tolerant load and the engine to agree on the
// six counters, the verdict and the re-saved bytes.
func TestLuauLoadKindMismatchAgreesWithTheEngine(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	for _, c := range loadFixIKindMismatchCases {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			loadFixICheck(t, c)
		})
	}
}
