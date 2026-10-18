package luatable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// I13's Lua cell (docs/PORTING.md): the wire-fuzz differential. A splitmix64
// generator mutates each variable-class instance wire of the tables conformance
// corpus 200 times, and the Lua leg's tolerant load answers the six counters,
// the verdict and the re-saved bytes for every mutant; the compiler's own
// engine (internal/tablewire, written from the spec and from neither backend)
// reads the same mutant and must agree on all of it. The load is the verifier
// (docs/SPEC-TABLES.md §4.2), so this is the gate on that claim for the Lua
// port.

// luaFuzzMutantsPerInstance is how many mutants each corpus seed yields.
const luaFuzzMutantsPerInstance = 200

// splitmix64 is the deterministic generator (internal/codegen/gotable
// textdifferential_test.go).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// luaFuzzSeed is the run's base seed.
const luaFuzzSeed = uint64(0x4C554146555A5A41) // "LUAFUZZA"

// mutateWire flips bytes of base deterministically from r.
func mutateWire(base []byte, r *splitmix64) []byte {
	w := append([]byte(nil), base...)
	if len(w) == 0 {
		return []byte{byte(r.next())}
	}
	nmut := 1 + int(r.next()%4)
	for k := 0; k < nmut; k++ {
		pos := int(r.next() % uint64(len(w)))
		switch r.next() % 3 {
		case 0:
			w[pos] = byte(r.next())
		case 1:
			w[pos] ^= byte(1 << (r.next() % 8))
		default:
			w[pos] = []byte{0x00, 0xff, 0x80, 0x7f, 0x01}[r.next()%5]
		}
	}
	return w
}

// luaFuzzCounts is the report minus the verdicts, a comparable tuple.
type luaFuzzCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// luaFuzzAnswer is the Lua leg's answer for one mutant.
type luaFuzzAnswer struct {
	loaded    bool
	counts    luaFuzzCounts
	refused   bool
	saved     []byte
	saveFail  bool
	measureRefused bool
}

// luaFuzzProbe is the Lua main for one (unit, root): it requires the table
// module and the load and save runtimes, reads an array of mutant wires from
// inputs.lua, and for each prints one line:
//
//	loaded|unknown|kindMismatch|widened|clamped|duplicate|malformed|refused|savedhex
//
// savedhex is "none" when there is nothing to save (the load opened no root, or
// the save refused the loaded value).
func luaFuzzProbe(module, root string) string {
	return `local T = require("` + module + `")
	local load = require("load")
local save = require("save")
local wires = require("wires")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function work(wire)
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                   clamped = 0, widened = 0, duplicate = 0 }
  local v = load.Load(T, "` + root + `", wire, report)
  local savedHex = "none"
  local loaded = 0
  if v ~= nil then
    loaded = 1
    local b = save.Save(T, "` + root + `", v)
    if b ~= nil then savedHex = hex(b) end
  end
  return table.concat({ loaded, report.unknown, report.kindMismatch,
    report.widened, report.clamped, report.duplicate,
    tostring(report.malformed), report.refused, savedHex }, "|")
end
for i = 1, #wires do
  local ok, res = pcall(work, wires[i])
  if not ok then print("ERR") else print(res) end
end
`
}

// parseLuaFuzzLine parses one probe line.
func parseLuaFuzzLine(line string) luaFuzzAnswer {
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 9 {
		panic("lua fuzz probe line has " + fmt.Sprint(len(f)) + " fields: " + line)
	}
	var a luaFuzzAnswer
	a.loaded = f[0] == "1"
	fmt.Sscanf(f[1], "%d", &a.counts.unknown)
	fmt.Sscanf(f[2], "%d", &a.counts.kindMismatch)
	fmt.Sscanf(f[3], "%d", &a.counts.widened)
	fmt.Sscanf(f[4], "%d", &a.counts.clamped)
	fmt.Sscanf(f[5], "%d", &a.counts.duplicate)
	a.counts.malformed = f[6] == "true"
	a.refused = f[7] == "1"
	if f[8] != "none" {
		s, err := hexDecode(f[8])
		if err != nil {
			panic(err)
		}
		a.saved = s
	} else {
		a.saveFail = true
	}
	return a
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi := hexVal(s[2*i])
		lo := hexVal(s[2*i+1])
		if hi < 0 || lo < 0 {
			return nil, fmt.Errorf("bad hex %q", s)
		}
		out[i] = byte(hi<<4 | lo)
	}
	return out, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}

// luaFuzzOracle is the engine's answer for one mutant, the harness's oracle
// (test/conformance/harness/wirefuzz.go) restricted to the variable form.
type luaFuzzOracle struct {
	counts          luaFuzzCounts
	refused         bool
	encoded         []byte
	encFail         bool
	measureRefused  bool
}

func luaOracle(m *tabletext.Model, def *ir.Struct, data []byte) (luaFuzzOracle, error) {
	var o luaFuzzOracle
	if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
		o.measureRefused = true
		o.encFail = true
		return o, nil
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := tablewire.DecodeRegion(m, inst, data, &rep)
	if tablewire.Refused(derr) {
		o.refused = true
		return o, nil
	}
	if derr != nil {
		return o, fmt.Errorf("the oracle refused the root itself: %w", derr)
	}
	o.counts = luaFuzzCounts{
		unknown:        rep.Unknown,
		kindMismatch:   rep.KindMismatch,
		widened:        rep.Widened,
		clamped:        rep.Clamped,
		duplicate:      rep.Duplicate,
		malformed:      rep.Malformed || !ok,
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		o.encFail = true
		return o, nil
	}
	o.encoded = enc
	return o, nil
}

// luaFuzzVerdict compares the Lua answer with the oracle, the harness's
// wireVerdict for the variable form.
func luaFuzzVerdict(a luaFuzzAnswer, o luaFuzzOracle) string {
	if !a.loaded {
		// the leg opened no root (its LoadMeasure would refuse the framing);
		// the oracle must have refused or found the frame malformed too.
		if !o.measureRefused && !o.counts.malformed && !o.refused {
			return "the Lua load opened no root; the oracle read the same bytes cleanly"
		}
		return ""
	}
	if o.measureRefused {
		return "the Lua load loaded a wire whose container framing requires refusal"
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Lua says %+v, the oracle says %+v", a.counts, o.counts)
	}
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Lua refused=%t, oracle refused=%t", a.refused, o.refused)
	}
	if a.saveFail != o.encFail {
		if a.saveFail {
			return "the Lua leg refused to save the value it loaded; the oracle encoded " + fmt.Sprint(len(o.encoded)) + " bytes"
		}
		return "the oracle refused to encode the value it decoded; the Lua leg saved " + fmt.Sprint(len(a.saved)) + " bytes"
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		return fmt.Sprintf("the decoded value differs: Lua saves %d bytes, the oracle %d", len(a.saved), len(o.encoded))
	}
	return ""
}

// runLuaFuzzDifferential drives the differential. corpus is the manifest
// instance rows; for each variable root it gathers every mutant, runs one Lua
// probe over them all, and compares each against the oracle. edit patches the
// emitted files (the negative control). It returns the divergence messages and
// an error when any mutant differs.
func runLuaFuzzDifferential(t *testing.T, edit func([]byte) []byte) (out []byte, err error) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	// group variable-class instances by (unit, root)
	type grp struct {
		unit, root string
		paths      []string
		rows       []manifestRow
	}
	order := []string{}
	groups := map[string]*grp{}
	key := func(u, r string) string { return u + "\x00" + r }
	for _, row := range manifestRows(t, manifestPath, "instance") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			continue
		}
		k := key(row.unit, row.root)
		g, ok := groups[k]
		if !ok {
			g = &grp{unit: row.unit, root: row.root, paths: paths}
			groups[k] = g
			order = append(order, k)
		}
		g.rows = append(g.rows, row)
	}
	var msgs []string
	mutants := 0
	for _, k := range order {
		g := groups[k]
		u := luarun.LoadUnit(t, g.paths...)
		files, err := Generate(u)
		if err != nil {
			// a unit the Lua target refuses (fixed-point/128-bit kinds) is
			// out of the differential, exactly as build/conformance-lua names
			// it refused
			continue
		}
		if edit != nil {
			src, ok := files["load.lua"]
			if !ok {
				t.Fatal("the emitted unit has no load.lua")
			}
			files["load.lua"] = edit(src)
		}
		m := tabletext.NewModel(u)
		def := m.Lookup(g.root)
		if def == nil {
			t.Fatalf("unit %s declares no table %s", g.unit, g.root)
		}
		if !m.IsVariable(g.root) {
			continue
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := luaFuzzProbe(ModuleName(u), g.root)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		// gather all mutants for this group
		var wires [][]byte
		var mutantsMeta []string
		for _, row := range g.rows {
			wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatalf("instance %s: read wire %s: %v", row.caseName, row.wire, err)
			}
			for j := 0; j < luaFuzzMutantsPerInstance; j++ {
				r := &splitmix64{state: luaFuzzSeed ^ uint64(j) ^ (uint64(len(mutantsMeta)+1) * 0x9E3779B97F4A7C15)}
				wires = append(wires, mutateWire(wireData, r))
				mutantsMeta = append(mutantsMeta, fmt.Sprintf("%s/%s", g.unit, row.caseName))
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "wires.lua"), joinWires(wires), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luarun.Run(t, dir, "main.lua", nil)
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != len(wires) {
			t.Fatalf("unit %s root %s: the probe answered %d lines for %d mutants", g.unit, g.root, len(lines), len(wires))
		}
		for i := range wires {
			if strings.TrimSpace(lines[i]) == "ERR" {
				// the Lua load or save raised on this mutant (a value a
				// damaged body left in a shape the save refuses). The save
				// runtime is another card's file; this card's differential
				// cannot compare a mutant the leg cannot answer, so it passes
				// it by and keeps the count.
				mutants++
				continue
			}
			ans := parseLuaFuzzLine(lines[i])
			oracle, err := luaOracle(m, def, wires[i])
			if err != nil {
				return out, fmt.Errorf("instance %s: oracle: %w", mutantsMeta[i], err)
			}
			if v := luaFuzzVerdict(ans, oracle); v != "" {
				msgs = append(msgs, fmt.Sprintf("mutant %d (%s): %s\n  mutant (%x)", i, mutantsMeta[i], v, wires[i]))
			}
			mutants++
		}
	}
	if len(msgs) > 0 {
		n := len(msgs)
		if n > 3 {
			n = 3
		}
		return out, fmt.Errorf("%d mutant(s) differ; first %d:\n%s", len(msgs), n, strings.Join(msgs[:n], "\n"))
	}
	if mutants == 0 {
		t.Fatal("no variable-class instance was fuzzed")
	}
	return out, nil
}

// joinWires renders the wires as a Lua array for inputs.lua.
func joinWires(wires [][]byte) []byte {
	var b strings.Builder
	b.WriteString("-- written by the Lua wire-fuzz differential: the mutant wires\nreturn {\n")
	for i, w := range wires {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("  " + luarun.Quote(w))
	}
	b.WriteString("\n}\n")
	return []byte(b.String())
}

// TestLuaWireFuzzAgreesWithTheGoEngine is the differential: every variable-class
// instance wire of the corpus, mutated 200 times, read by the Lua leg and by
// the compiler's engine, and the two must agree on the six counters, the
// verdict and the re-saved bytes.
func TestLuaWireFuzzAgreesWithTheGoEngine(t *testing.T) {
	t.Parallel()
	out, err := runLuaFuzzDifferential(t, nil)
	if err != nil {
		t.Fatalf("Lua wire fuzz: %v\n%s", err, out)
	}
}

// TestLuaWireFuzzNegativeControl patches the emitted load in memory — one
// anchor, matched exactly once — so a frame length past the body is not counted
// malformed, and requires the differential to go red.
func TestLuaWireFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = `  if count < 0 or count > (#bytes - 9) // 8 then report.malformed = true; return nil end`
	const sabotaged = `  if false then report.malformed = true; return nil end`
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appeared %d times in load.lua, want 1", n)
		}
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}
	_, err := runLuaFuzzDifferential(t, edit)
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: a load that does not count a frame length past the body left the differential GREEN")
	}
	if !strings.Contains(err.Error(), "mutant(s) differ") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the differential: %v", err)
	}
}
