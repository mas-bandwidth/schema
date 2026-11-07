package luatable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUA WIRE-FUZZ DIFFERENTIAL (docs/PORT-LUA.md, THE CARD MAP:
// schema-lua-fuzz-differential; docs/SPEC-TABLES.md §4.2, "the read is the
// verifier"). A splitmix64 generator mutates every instance wire of the tables
// conformance corpus luaFuzzMutantsPerInstance times, the Lua leg's tolerant
// load answers the six counters, the verdict and the re-saved bytes for each
// mutant, and the compiler's own engine (internal/tablewire, written from the
// spec and from neither backend) reads the same bytes and must agree on all of
// it. This is the in-package half; `make tables-lua-wire-fuzz` drives the same
// load through the harness on its pipe (test/conformance/lua/wirefuzz.lua).

// luaFuzzMutantsPerInstance is how many mutants each corpus instance yields.
const luaFuzzMutantsPerInstance = 200

// luaFuzzSeed is the run's base seed ("LUAFUZZA"); every mutant's own seed is
// derived from it, the case name and the mutant's ordinal, so a red names a
// seed that rebuilds exactly that mutant.
const luaFuzzSeed = uint64(0x4C554146555A5A41)

// luaFuzzMutantSeed is the seed of mutant j of one corpus case.
func luaFuzzMutantSeed(caseName string, j int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(caseName))
	return luaFuzzSeed ^ h.Sum64() ^ (uint64(j+1) * 0x9E3779B97F4A7C15)
}

// luaFuzzMutate derives one mutant of base from seed: one to four byte edits
// (a random byte, one flipped bit, or a boundary value 0x00 0x01 0x7f 0x80
// 0xff), and one mutant in eight is also cut SHORT at a random length, so the
// trailer, the id table and every frame length meet a buffer that ends early.
func luaFuzzMutate(base []byte, seed uint64) []byte {
	r := &splitmix64{state: seed}
	w := append([]byte(nil), base...)
	if len(w) == 0 {
		return []byte{byte(r.next())}
	}
	edits := 1 + int(r.next()%4)
	for range edits {
		pos := int(r.next() % uint64(len(w)))
		switch r.next() % 3 {
		case 0:
			w[pos] = byte(r.next())
		case 1:
			w[pos] ^= byte(1 << (r.next() % 8))
		default:
			w[pos] = []byte{0x00, 0x01, 0x7f, 0x80, 0xff}[r.next()%5]
		}
	}
	if r.next()%8 == 0 {
		w = w[:int(r.next()%uint64(len(w)))]
	}
	return w
}

// luaFuzzCounts is the report's six counters, a comparable tuple.
type luaFuzzCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// luaFuzzAnswer is the Lua leg's answer for one mutant.
type luaFuzzAnswer struct {
	raised   string // the Lua error when the load or the save raised
	loaded   bool   // Load returned a root
	counts   luaFuzzCounts
	refused  bool
	saved    []byte
	saveFail bool // Save refused the loaded value (or nothing was loaded)
}

// luaFuzzProbe is the Lua main for one (unit, root): it requires the table
// module and the load and save runtimes, reads the mutants from the file its
// argument names (luaFuzzWiresFile), and
// prints one line per mutant:
//
//	loaded|unknown|kindMismatch|widened|clamped|duplicate|malformed|refused|savedhex
//
// savedhex is "none" when the load returned no root or the save refused. A load
// or save that RAISES prints "ERR|<hex of the message>": a raise is an answer
// the leg owes no caller, so the differential counts it as a divergence.
func luaFuzzProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local f = assert(io.open(arg[1], "rb"))
local blob = f:read("a")
f:close()
local wires = {}
do
  local n, at = string.unpack("<I4", blob, 1)
  for i = 1, n do
    local len
    len, at = string.unpack("<I4", blob, at)
    wires[i] = blob:sub(at, at + len - 1)
    at = at + len
  end
end
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
-- THE CALLER-OWNED LOAD (docs/PORTING.md M1) rides every mutant: one state and
-- one value are kept across the whole run, so each mutant is read into the
-- value the one before it left, and must answer exactly what the plain load
-- answers: the same report, the same root or none, the same re-saved bytes.
local state, into = load.NewState(), T.New` + root + `()
local function work(wire)
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                   clamped = 0, widened = 0, duplicate = 0 }
  local v = load.LoadTolerant(T, "` + root + `", wire, report)
  local savedHex = "none"
  local loaded = 0
  if v ~= nil then
    loaded = 1
    local b = save.Save(T, "` + root + `", v)
    if b ~= nil then savedHex = hex(b) end
  end
  local report2 = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                    clamped = 0, widened = 0, duplicate = 0 }
  local v2 = load.LoadTolerant(T, "` + root + `", wire, report2, state, into)
  local saved2 = "none"
  if v2 ~= nil then
    local b = save.Save(T, "` + root + `", v2)
    if b ~= nil then saved2 = hex(b) end
  end
  for _, k in ipairs({ "malformed", "refused", "unknown", "kindMismatch", "clamped", "widened", "duplicate" }) do
    if report[k] ~= report2[k] then error("the caller-owned load counted " .. k .. " differently") end
  end
  if (report.nonFiniteFloat == nil) ~= (report2.nonFiniteFloat == nil) or (v == nil) ~= (v2 == nil) or saved2 ~= savedHex then
    error("the caller-owned load read a different value")
  end
  return table.concat({ loaded, report.unknown, report.kindMismatch,
    report.widened, report.clamped, report.duplicate,
    tostring(report.malformed), report.nonFiniteFloat ~= nil and 1 or report.refused, savedHex }, "|")
end
for i = 1, #wires do
  local ok, res = pcall(work, wires[i])
  if ok then print(res) else print("ERR|" .. hex(tostring(res))) end
end
`
}

// parseLuaFuzzLine parses one probe line.
func parseLuaFuzzLine(line string) (luaFuzzAnswer, error) {
	var a luaFuzzAnswer
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) == 2 && f[0] == "ERR" {
		msg, err := hex.DecodeString(f[1])
		if err != nil {
			return a, fmt.Errorf("probe ERR line %q: %w", line, err)
		}
		a.raised = string(msg)
		return a, nil
	}
	if len(f) != 9 {
		return a, fmt.Errorf("probe line has %d fields, want 9: %q", len(f), line)
	}
	a.loaded = f[0] == "1"
	n := []*int{&a.counts.unknown, &a.counts.kindMismatch, &a.counts.widened, &a.counts.clamped, &a.counts.duplicate}
	for i, p := range n {
		if _, err := fmt.Sscanf(f[1+i], "%d", p); err != nil {
			return a, fmt.Errorf("probe line %q field %d: %w", line, 1+i, err)
		}
	}
	a.counts.malformed = f[6] == "true"
	a.refused = f[7] != "0"
	if f[8] == "none" {
		a.saveFail = true
		return a, nil
	}
	s, err := hex.DecodeString(f[8])
	if err != nil {
		return a, fmt.Errorf("probe line %q saved bytes: %w", line, err)
	}
	a.saved = s
	return a, nil
}

// luaFuzzOracle is the engine's answer for one mutant: the harness's oracle
// (test/conformance/harness/wirefuzz.go, wireRoot.oracle) for a file root.
type luaFuzzOracle struct {
	variable       bool // the root is variable-class (§3.1)
	framingRefused bool // FileRegionMeasure refuses to size a region (§6.5)
	noRoot         bool // the engine refused the root itself
	readNothing    bool // the engine's whole answer is one malformed event and the defaults (§3: an unreadable trailer, bytes after the root)
	counts         luaFuzzCounts
	refused        bool
	// nonFinite is the engine's REFUSAL of a float whose bits are a NaN or an
	// infinity (docs/SPEC-TABLES.md §3.3): every read refuses it, the
	// tolerant one too, so the whole of what the leg owes is the refusal.
	nonFinite bool
	encoded   []byte
	encFail   bool
}

// luaFuzzEngine reads one mutant with the compiler's engine. A variable root's
// region is measured first, as a region caller measures (§6.5); a wire the
// framing refuses is read by the plain Decode, because a Lua load has no
// region to size and still owes its report for the same bytes.
func luaFuzzEngine(m *tabletext.Model, def *ir.Struct, data []byte) luaFuzzOracle {
	o := luaFuzzOracle{variable: m.IsVariable(def.Name)}
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
	if _, refused := errors.AsType[*tablewire.NonFiniteRefusal](derr); refused {
		o.nonFinite = true
		return o
	}
	switch {
	case tablewire.Refused(derr):
		o.refused = true
	case derr != nil:
		o.noRoot = true
		return o
	default:
		o.counts = luaFuzzCounts{
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
	if o.counts == (luaFuzzCounts{malformed: true}) {
		fresh, ferr := tablewire.Encode(m, m.New(def))
		o.readNothing = ferr == nil && bytes.Equal(fresh, enc)
	}
	return o
}

// luaFuzzVerdict compares the Lua answer with the engine's and names the first
// disagreement, or "" when there is none; it is the harness's wireVerdict
// (test/conformance/harness/wirefuzz.go) for a file root. A FIXED root always
// loads (test/conformance/README.md, "The wire-fuzz driver": its Load fills a
// value and reports), so no root there is a divergence. A VARIABLE root's load
// that returns NO ROOT plays the part of a LoadMeasure refusal (the Lua leg has
// no region to measure), and agrees only with a wire the engine also could not
// read: refused by form, refused by the framing, or read as nothing but one
// malformed event and the defaults; the verdict must still match, and the
// counters are not compared, because the leg read nothing. A wire the engine
// itself cannot take as a root is always a divergence (the harness fails the
// run there too).
//
// ONE BRANCH IS NOT wireVerdict's: where FileRegionMeasure refuses the framing,
// the harness reds any leg that loads, because a region leg must not. The Lua
// load has no region, so here a load of such a wire is compared with the
// engine's plain Decode instead (luaFuzzEngine); the harness run still holds
// the leg to the refusal once the runtime carries a LoadMeasure.
func luaFuzzVerdict(a luaFuzzAnswer, o luaFuzzOracle) string {
	if a.raised != "" {
		return "the Lua leg RAISED: " + a.raised
	}
	if o.nonFinite {
		// A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md
		// §3.3): the leg lands nothing and says so; nothing it counted on the
		// way is compared, because nothing of it lands.
		if a.loaded || !a.refused {
			return "the engine refuses a non-finite float (non_finite_float); the Lua load did not"
		}
		return ""
	}
	if o.noRoot {
		return "the engine refused the root itself"
	}
	if o.counts.malformed && !a.refused {
		// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the leg
		// owes the refusal, and no counter or saved byte is compared
		if !a.counts.malformed && a.loaded {
			return fmt.Sprintf("the engine discards a damaged load: Lua says %+v, the engine says %+v", a.counts, o.counts)
		}
		return ""
	}
	if !a.loaded {
		if !o.variable {
			return "the Lua load returned no root for a FIXED root, which always loads"
		}
		if a.refused != o.refused {
			return fmt.Sprintf("the Lua load returned no root and the verdict differs: Lua refused=%t, the engine refused=%t", a.refused, o.refused)
		}
		if o.refused || o.framingRefused || o.readNothing {
			return ""
		}
		return fmt.Sprintf("the Lua load returned no root; the engine read a value (%+v)", o.counts)
	}
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Lua refused=%t, the engine refused=%t", a.refused, o.refused)
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Lua says %+v, the engine says %+v", a.counts, o.counts)
	}
	if a.saveFail != o.encFail {
		if a.saveFail {
			return fmt.Sprintf("the Lua save refused the value it loaded; the engine encoded %d bytes", len(o.encoded))
		}
		return fmt.Sprintf("the engine refused to encode the value it decoded; the Lua save wrote %d bytes", len(a.saved))
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		at := 0
		for at < len(a.saved) && at < len(o.encoded) && a.saved[at] == o.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: Lua saves %d bytes, the engine %d, first difference at byte %d\n    lua    %x\n    engine %x",
			len(a.saved), len(o.encoded), at, a.saved, o.encoded)
	}
	return ""
}

// luaFuzzResult is one run of the differential.
type luaFuzzResult struct {
	mutants, roots, framingRefused int
	divergences                    []string
}

// luaFuzzGroup is one (unit, root) of the corpus: its generated unit in a
// temporary directory, the engine's model, and every mutant of its instances.
type luaFuzzGroup struct {
	unit, root string
	dir        string
	m          *tabletext.Model
	def        *ir.Struct
	mutants    []luaFuzzMutant
	chunks     []*luaFuzzChunk
}

// luaFuzzChunk is one Lua process's share of a group: at most
// luaFuzzChunkBytes of mutant wire, so a corpus instance of hundreds of
// kilobytes (tabledemo wide_blob) is spread over several processes instead of
// serialising the run behind one.
type luaFuzzChunk struct {
	file string
	n    int
	out  []byte
	err  error
}

// luaFuzzChunkBytes bounds the wire bytes one probe process reads.
const luaFuzzChunkBytes = 1 << 20

// luaFuzzMutant is one mutant and the seed that rebuilds it.
type luaFuzzMutant struct {
	caseName string
	seed     uint64
	wire     []byte
}

// runLuaFuzzDifferential drives the differential over every instance root of
// the corpus whose unit the Lua target generates. edit, when non-nil, patches
// the emitted load.lua of every unit (the negative control). The groups are
// staged in order, their Lua probes run concurrently (each a separate process
// in its own t.TempDir(), no network and no clock), and every answer is then
// compared with the engine in order, so a red is deterministic.
func runLuaFuzzDifferential(t *testing.T, edit func([]byte) []byte) luaFuzzResult {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	type rows struct {
		unit, root string
		rows       []manifestRow
	}
	var order []*rows
	byKey := map[string]*rows{}
	for _, row := range manifestRows(t, manifestPath, "instance") {
		k := row.unit + "\x00" + row.root
		g := byKey[k]
		if g == nil {
			g = &rows{unit: row.unit, root: row.root}
			byKey[k] = g
			order = append(order, g)
		}
		g.rows = append(g.rows, row)
	}
	var groups []*luaFuzzGroup
	for _, r := range order {
		paths, ok := units[r.unit]
		if !ok {
			t.Fatalf("instance root %s names unit %s, which the manifest does not declare", r.root, r.unit)
		}
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			// A UNIT THE LUA TARGET REFUSES BY NAME (the fixed-point and
			// 128-bit table kinds, docs/SPEC-TABLES.md §15) is out of the
			// differential, exactly as build/conformance-lua names it refused.
			continue
		}
		if edit != nil {
			src, ok := files["load.lua"]
			if !ok {
				t.Fatal("the emitted unit has no load.lua")
			}
			files["load.lua"] = edit(src)
		}
		g := &luaFuzzGroup{unit: r.unit, root: r.root, dir: t.TempDir(), m: tabletext.NewModel(u)}
		if g.def = g.m.Lookup(r.root); g.def == nil {
			t.Fatalf("unit %s declares no table %s", r.unit, r.root)
		}
		luarun.WriteFiles(t, g.dir, files)
		if err := os.WriteFile(filepath.Join(g.dir, "main.lua"), []byte(luaFuzzProbe(ModuleName(u), r.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, row := range r.rows {
			base, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatalf("instance %s: %v", row.caseName, err)
			}
			for j := range luaFuzzMutantsPerInstance {
				seed := luaFuzzMutantSeed(row.caseName, j)
				g.mutants = append(g.mutants, luaFuzzMutant{row.caseName, seed, luaFuzzMutate(base, seed)})
			}
		}
		for at := 0; at < len(g.mutants); {
			end, size := at, 0
			for end < len(g.mutants) && (end == at || size+len(g.mutants[end].wire) <= luaFuzzChunkBytes) {
				size += len(g.mutants[end].wire)
				end++
			}
			c := &luaFuzzChunk{file: fmt.Sprintf("wires-%d.bin", len(g.chunks)), n: end - at}
			if err := os.WriteFile(filepath.Join(g.dir, c.file), luaFuzzWiresFile(g.mutants[at:end]), 0o644); err != nil {
				t.Fatal(err)
			}
			g.chunks = append(g.chunks, c)
			at = end
		}
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		t.Fatal("no instance root of the corpus was staged")
	}
	lua := luarun.Interpreter(t)
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(1, goruntime.GOMAXPROCS(0)/2))
	for _, g := range groups {
		for _, c := range g.chunks {
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				c.out, c.err = luaFuzzRun(lua, g.dir, c.file)
			})
		}
	}
	wg.Wait()
	var res luaFuzzResult
	for _, g := range groups {
		var lines []string
		for _, c := range g.chunks {
			if c.err != nil {
				t.Fatalf("unit %s root %s: %v", g.unit, g.root, c.err)
			}
			got := strings.Split(strings.TrimRight(string(c.out), "\n"), "\n")
			if len(got) != c.n {
				t.Fatalf("unit %s root %s: the probe answered %d lines for %d mutants of %s", g.unit, g.root, len(got), c.n, c.file)
			}
			lines = append(lines, got...)
		}
		if len(lines) != len(g.mutants) {
			t.Fatalf("unit %s root %s: the probe answered %d lines for %d mutants", g.unit, g.root, len(lines), len(g.mutants))
		}
		res.roots++
		for i, mu := range g.mutants {
			ans, err := parseLuaFuzzLine(lines[i])
			if err != nil {
				t.Fatalf("unit %s root %s: %v", g.unit, g.root, err)
			}
			o := luaFuzzEngine(g.m, g.def, mu.wire)
			if o.framingRefused {
				res.framingRefused++
			}
			res.mutants++
			if v := luaFuzzVerdict(ans, o); v != "" {
				res.divergences = append(res.divergences, fmt.Sprintf("seed %#016x unit %s root %s case %s: %s\n    mutant %x",
					mu.seed, g.unit, g.root, mu.caseName, v, mu.wire))
			}
		}
	}
	return res
}

// luaFuzzRun runs one chunk's probe under the pinned interpreter with its
// directory as the working directory and the whole of package.path, the
// environment luarun.Run gives a probe, and returns stdout.
func luaFuzzRun(lua, dir, wires string) ([]byte, error) {
	cmd := exec.Command(lua, "main.lua", wires)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LUA_PATH="+filepath.Join(dir, "?.lua")+";;", "LUA_INIT=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("lua main.lua: %w\nstderr:\n%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// luaFuzzWiresFile is the mutants as the probe reads them: a little-endian u32
// count, then per mutant a u32 length and its bytes.
func luaFuzzWiresFile(mutants []luaFuzzMutant) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(mutants)))
	for _, mu := range mutants {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(mu.wire)))
		b = append(b, mu.wire...)
	}
	return b
}

// luaFuzzCost is what the slow half pays for these tests (internal/slowtest):
// the pinned Lua interpreter over every mutant, tens of CPU-seconds, most of it
// tabledemo wide_blob's 210 KB instance read 200 times.
const luaFuzzCost = "the pinned Lua interpreter (dist/lua-5.4.8, make dist-lua) over every mutant of the corpus"

// luaFuzzSummary is the run's one line.
func luaFuzzSummary(r luaFuzzResult) string {
	return fmt.Sprintf("Lua wire fuzz: %d mutants over %d roots (%d the framing refuses), %d divergences",
		r.mutants, r.roots, r.framingRefused, len(r.divergences))
}

// TestLuaWireFuzzAgreesWithTheGoEngine is the differential: every instance wire
// of the corpus, mutated luaFuzzMutantsPerInstance times, read by the Lua leg
// and by the compiler's engine, and the two must agree on the six counters,
// the verdict and the re-saved bytes of every mutant (docs/SPEC-TABLES.md
// §4.2).
func TestLuaWireFuzzAgreesWithTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaFuzzCost)
	res := runLuaFuzzDifferential(t, nil)
	t.Log(luaFuzzSummary(res))
	if path := os.Getenv("SCHEMA_LUA_FUZZ_DIVERGENCES"); path != "" {
		// every divergence, one per paragraph, for a reader classifying a red
		if err := os.WriteFile(path, []byte(strings.Join(res.divergences, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(res.divergences) > 0 {
		n := min(len(res.divergences), 5)
		t.Fatalf("%s; first %d:\n%s", luaFuzzSummary(res), n, strings.Join(res.divergences[:n], "\n"))
	}
}

// luaFuzzControlAnchor is the string field's frame check in the emitted load
// (runtime/load.lua, read_payload, the code a field and a union arm are both
// read by): a length past the body is malformed (§3). The "string" branch is
// matched so the anchor is unique (the "wstring" branch carries the same frame
// check but a different elseif line).
const luaFuzzControlAnchor = `  elseif s == "string" then
    if n == nil then
      n = r_leb(r)
      if r.bad or not fits(r, n) then
        report.malformed = true
        return nil, STOP
      end
    end`

// TestLuaWireFuzzNegativeControl patches the emitted load in memory — one
// anchor, matched exactly once — so a string frame length past the body stops
// the body WITHOUT counting it malformed, and requires the differential to go
// red on exactly that.
func TestLuaWireFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaFuzzCost)
	sabotaged := strings.Replace(luaFuzzControlAnchor, "report.malformed = true\n", "", 1)
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), luaFuzzControlAnchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in load.lua, want 1", n)
		}
		return []byte(strings.Replace(string(src), luaFuzzControlAnchor, sabotaged, 1))
	}
	clean := runLuaFuzzDifferential(t, nil)
	res := runLuaFuzzDifferential(t, edit)
	t.Logf("clean: %s", luaFuzzSummary(clean))
	t.Logf("sabotaged: %s", luaFuzzSummary(res))
	if len(res.divergences) <= len(clean.divergences) {
		t.Fatalf("NEGATIVE CONTROL FAILED: a load that does not count a string frame past the body left the differential at %d divergences (clean %d)",
			len(res.divergences), len(clean.divergences))
	}
	before := map[string]bool{}
	for _, d := range clean.divergences {
		before[d] = true
	}
	removed := regexp.MustCompile(`Lua says \{[^}]*malformed:false\}, the engine says \{[^}]*malformed:true\}`)
	for _, d := range res.divergences {
		if !before[d] && removed.MatchString(d) {
			return
		}
	}
	t.Fatal("NEGATIVE CONTROL FAILED: the differential went red, but no divergence is the malformed count the sabotage removed")
}
