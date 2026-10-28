package luautable_test

// THE LUAU WIRE-FUZZ DIFFERENTIAL (docs/PORT-LUAU.md, THE CARD MAP:
// schema-luau-fuzz-differential; docs/SPEC-TABLES.md §4.2, "the read is the
// verifier"). A splitmix64 generator mutates every instance wire of the tables
// conformance corpus luauFuzzMutantsPerInstance times, the Luau leg's tolerant
// load answers the six counters, the verdict and the re-saved bytes for each
// mutant, and the compiler's own engine (internal/tablewire, written from the
// spec and from neither backend) reads the same bytes and must agree on all of
// it. This is the in-package half; `make tables-luau-wire-fuzz` drives the same
// load through the harness on its pipe (test/conformance/luau/wirefuzz.luau).

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauFuzzMutantsPerInstance is how many mutants each corpus instance yields.
const luauFuzzMutantsPerInstance = 200

// luauFuzzSeed is the run's base seed ("LUAUFUZZ"); every mutant's own seed
// is derived from it, the case name and the mutant's ordinal, so a red names a
// seed that rebuilds exactly that mutant.
const luauFuzzSeed = uint64(0x4C554146555A5A41)

// luauFuzzMutantSeed is the seed of mutant j of one corpus case.
func luauFuzzMutantSeed(caseName string, j int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(caseName))
	return luauFuzzSeed ^ h.Sum64() ^ (uint64(j+1) * 0x9E3779B97F4A7C15)
}

// luauFuzzMutate derives one mutant of base from seed: one to four byte edits
// (a random byte, one flipped bit, or a boundary value 0x00 0x01 0x7f 0x80
// 0xff), and one mutant in eight is also cut SHORT at a random length, so the
// trailer, the id table and every frame length meet a buffer that ends early.
func luauFuzzMutate(base []byte, seed uint64) []byte {
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

// splitmix64 is the generator docs/PORT-LUAU.md's methods name (the one
// internal/codegen/gotable/textdifferential_test.go uses).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// luauFuzzCounts is the report's six counters, a comparable tuple.
type luauFuzzCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// luauFuzzAnswer is the Luau leg's answer for one mutant, as the driver's one
// hex line frames it (the harness's 47-byte header).
type luauFuzzAnswer struct {
	raised   string // the Luau error when the load or the save raised
	loaded   bool   // Load returned a root
	counts   luauFuzzCounts
	refused  bool
	saved    []byte
	saveFail bool // Save refused the loaded value (or nothing was loaded)
}

// parseLuauFuzzLine parses one reply line: the 47-byte header plus the saved
// bytes, lowercase hex.
func parseLuauFuzzLine(line string) (luauFuzzAnswer, error) {
	var a luauFuzzAnswer
	line = strings.TrimSpace(line)
	if line == "ERR" {
		a.raised = "the Luau load or save raised"
		return a, nil
	}
	raw, err := hex.DecodeString(line)
	if err != nil {
		return a, fmt.Errorf("reply line is not lowercase hex: %w", err)
	}
	if len(raw) < 47 {
		return a, fmt.Errorf("reply has %d bytes, want at least the 47-byte header", len(raw))
	}
	a.loaded = raw[0] != 0
	a.counts = luauFuzzCounts{
		unknown:      int(int32(binary.LittleEndian.Uint32(raw[1:]))),
		kindMismatch: int(int32(binary.LittleEndian.Uint32(raw[5:]))),
		widened:      int(int32(binary.LittleEndian.Uint32(raw[9:]))),
		clamped:      int(int32(binary.LittleEndian.Uint32(raw[13:]))),
		duplicate:    int(int32(binary.LittleEndian.Uint32(raw[17:]))),
		malformed:    raw[21] != 0,
	}
	a.refused = raw[22] != 0
	n := int64(binary.LittleEndian.Uint64(raw[39:]))
	if n < 0 {
		a.saveFail = true
		return a, nil
	}
	if int64(len(raw)) != 47+n {
		return a, fmt.Errorf("reply has %d bytes, the header names %d saved", len(raw), n)
	}
	a.saved = raw[47:]
	return a, nil
}

// luauFuzzOracle is the engine's answer for one mutant: the harness's oracle
// (test/conformance/harness/wirefuzz.go, wireRoot.oracle) for a file root.
type luauFuzzOracle struct {
	variable       bool // the root is variable-class (§3.1)
	framingRefused bool // FileRegionMeasure refuses to size a region (§6.5)
	noRoot         bool // the engine refused the root itself
	// nonFinite is the engine's REFUSAL of a float whose bits are a NaN or an
	// infinity (docs/SPEC-TABLES.md §3.3): every read refuses it, tolerant and
	// strict, so the whole of what the leg owes is the refusal itself (the
	// harness's oracleAnswer.nonFinite).
	nonFinite   bool
	readNothing bool // the engine's whole answer is one malformed event and the defaults (§3: an unreadable trailer, bytes after the root)
	counts      luauFuzzCounts
	refused     bool
	encoded     []byte
	encFail     bool
}

// luauFuzzEngine reads one mutant with the compiler's engine. A variable
// root's region is measured first, as a region caller measures (§6.5); a wire
// the framing refuses is read by the plain Decode, because a Luau load has no
// region to size and still owes its report for the same bytes.
func luauFuzzEngine(m *tabletext.Model, def *ir.Struct, data []byte) luauFuzzOracle {
	o := luauFuzzOracle{variable: m.IsVariable(def.Name)}
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
		o.counts = luauFuzzCounts{
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
	if o.counts == (luauFuzzCounts{malformed: true}) {
		fresh, ferr := tablewire.Encode(m, m.New(def))
		o.readNothing = ferr == nil && bytes.Equal(fresh, enc)
	}
	return o
}

// luauFuzzVerdict compares the Luau answer with the engine's and names the
// first disagreement, or "" when there is none; it is the harness's wireVerdict
// (test/conformance/harness/wirefuzz.go) for a file root. A FIXED root always
// loads (test/conformance/README.md, "The wire-fuzz driver": its Load fills a
// value and reports), so no root there is a divergence. A VARIABLE root's load
// that returns NO ROOT plays the part of a LoadMeasure refusal (the Luau leg
// has no region to measure), and agrees only with a wire the engine also could
// not read: refused by form, refused by the framing, or read as nothing but one
// malformed event and the defaults; the verdict must still match, and the
// counters are not compared, because the leg read nothing. A wire the engine
// itself cannot take as a root is always a divergence (the harness fails the
// run there too).
func luauFuzzVerdict(a luauFuzzAnswer, o luauFuzzOracle) string {
	if a.raised != "" {
		return "the Luau leg RAISED: " + a.raised
	}
	if o.nonFinite {
		// A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md
		// §3.3), and the refusal is the answer: nothing the leg decoded or
		// counted on the way is compared, because nothing of it lands.
		if !a.refused {
			return "the engine refuses a non-finite float (non_finite_float); the Luau load did not refuse the wire"
		}
		return ""
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
		if o.refused || o.framingRefused || o.counts.malformed || o.readNothing {
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
			return fmt.Sprintf("the Luau save refused the value it loaded; the engine encoded %d bytes", len(o.encoded))
		}
		return fmt.Sprintf("the engine refused to encode the value it decoded; the Luau save wrote %d bytes", len(a.saved))
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		at := 0
		for at < len(a.saved) && at < len(o.encoded) && a.saved[at] == o.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: Luau saves %d bytes, the engine %d, first difference at byte %d\n    luau   %x\n    engine %x",
			len(a.saved), len(o.encoded), at, a.saved, o.encoded)
	}
	return ""
}

// luauFuzzResult is one run of the differential.
type luauFuzzResult struct {
	mutants, roots, framingRefused int
	divergences                    []string
}

// luauFuzzMutant is one mutant and the seed that rebuilds it.
type luauFuzzMutant struct {
	caseName string
	seed     uint64
	wire     []byte
}

// luauFuzzGroup is one (unit, root) of the corpus: its generated unit staged
// in a temporary directory, the engine's model, and every mutant of its
// instances.
type luauFuzzGroup struct {
	unit, root string
	dir        string
	m          *tabletext.Model
	def        *ir.Struct
	mods       []string
	mutants    []luauFuzzMutant
	chunkList  []*luauFuzzChunk
}

// luauFuzzChunkBytes bounds the wire bytes one probe process reads, so a corpus
// instance of hundreds of kilobytes (tabledemo wide_blob) is spread over
// several processes instead of serialising the run behind one.
const luauFuzzChunkBytes = 1 << 20

// luauFuzzChunk is one Luau process's share of a group: at most
// luauFuzzChunkBytes of mutant wire, so the probe's inputs.luau stays a size
// the interpreter parses quickly.
type luauFuzzChunk struct {
	n       int
	mutants []luauFuzzMutant
	out     []byte
	err     error
}

// chunks splits the group's mutants into chunks by wire bytes.
func (g *luauFuzzGroup) chunks() []*luauFuzzChunk {
	if g.chunkList != nil {
		return g.chunkList
	}
	for at := 0; at < len(g.mutants); {
		end, size := at, 0
		for end < len(g.mutants) && (end == at || size+len(g.mutants[end].wire) <= luauFuzzChunkBytes) {
			size += len(g.mutants[end].wire)
			end++
		}
		g.chunkList = append(g.chunkList, &luauFuzzChunk{n: end - at, mutants: g.mutants[at:end]})
		at = end
	}
	return g.chunkList
}

// runLuauFuzzDifferential drives the differential over every instance root of
// the corpus whose unit the Luau target generates. edit, when non-nil, patches
// the emitted schema_load.luau of every unit (the negative control). The groups are
// staged in order, their probes run concurrently (each a separate process in
// its own t.TempDir(), no network and no clock), and every answer is then
// compared with the engine in order, so a red is deterministic.
func runLuauFuzzDifferential(t *testing.T, edit func([]byte) []byte) luauFuzzResult {
	t.Helper()
	units, _ := readManifest(t)
	type rows struct {
		unit, root string
		rows       []reportRow
	}
	var order []*rows
	byKey := map[string]*rows{}
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		k := row.unit + "\x00" + row.root
		g := byKey[k]
		if g == nil {
			g = &rows{unit: row.unit, root: row.root}
			byKey[k] = g
			order = append(order, g)
		}
		g.rows = append(g.rows, row)
	}

	driver, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", "wirefuzz.luau"))
	if err != nil {
		t.Fatal(err)
	}
	type generated struct {
		files map[string][]byte
		m     *tabletext.Model
	}
	cache := map[string]*generated{}
	filesFor := func(unitKey string) *generated {
		if g, ok := cache[unitKey]; ok {
			return g
		}
		paths, ok := units[unitKey]
		if !ok {
			t.Fatalf("instance root names unit %s, which the manifest does not declare", unitKey)
		}
		schemaPaths, err := compiler.GatherPaths(prefix(paths))
		if err != nil {
			t.Fatalf("unit %s: %v", unitKey, err)
		}
		u, err := compiler.New().Load(schemaPaths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", unitKey, err)
		}
		files, err := luau.Generate(u)
		if err != nil {
			cache[unitKey] = nil
			return nil
		}
		tables, err := luautable.Generate(u)
		if err != nil {
			// A UNIT THE LUAU TARGET REFUSES BY NAME IS OUT OF THE
			// DIFFERENTIAL, exactly as build/conformance-luau names it refused.
			cache[unitKey] = nil
			return nil
		}
		maps.Copy(files, tables)
		g := &generated{files: files, m: tabletext.NewModel(u)}
		cache[unitKey] = g
		return g
	}

	var groups []*luauFuzzGroup
	for _, r := range order {
		gen := filesFor(r.unit)
		if gen == nil {
			continue
		}
		g := &luauFuzzGroup{unit: r.unit, root: r.root, dir: t.TempDir(), m: gen.m}
		if g.def = g.m.Lookup(r.root); g.def == nil {
			t.Fatalf("unit %s declares no table %s", r.unit, r.root)
		}
		files := make(map[string][]byte, len(gen.files)+1)
		for name, data := range gen.files {
			if name == "schema_load.luau" && edit != nil {
				data = edit(data)
			}
			files[name] = data
		}
		files["wirefuzz.luau"] = driver
		var mods []string
		for name := range files {
			if strings.HasSuffix(name, "Table.luau") {
				mods = append(mods, strings.TrimSuffix(name, ".luau"))
			}
		}
		sort.Strings(mods)
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, row := range r.rows {
			base, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatalf("instance %s: %v", row.caseName, err)
			}
			for j := range luauFuzzMutantsPerInstance {
				seed := luauFuzzMutantSeed(row.caseName, j)
				g.mutants = append(g.mutants, luauFuzzMutant{row.caseName, seed, luauFuzzMutate(base, seed)})
			}
		}
		g.mods = mods
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		t.Fatal("no instance root of the corpus was staged")
	}
	luau, why := luaurun.Interpreter()
	if luau == "" {
		t.Fatalf("luaurun: the Luau interpreter does not resolve: %s", why)
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(1, goruntime.GOMAXPROCS(0)/2))
	for _, g := range groups {
		wg.Add(1)
		go func(g *luauFuzzGroup) {
			defer wg.Done()
			// one chunk at a time per group: the probe's inputs.luau lives
			// in the group's directory, and the group's schema modules are
			// only staged there once.
			for _, c := range g.chunks() {
				slots <- struct{}{}
				c.out, c.err = luauFuzzRun(luau, g, c)
				<-slots
			}
		}(g)
	}
	wg.Wait()
	var res luauFuzzResult
	for _, g := range groups {
		var lines []string
		for _, c := range g.chunkList {
			if c.err != nil {
				t.Fatalf("unit %s root %s: %v", g.unit, g.root, c.err)
			}
			got := strings.Split(strings.TrimRight(string(c.out), "\n"), "\n")
			if len(got) != c.n {
				t.Fatalf("unit %s root %s: the probe answered %d lines for %d mutants", g.unit, g.root, len(got), c.n)
			}
			lines = append(lines, got...)
		}
		if len(lines) != len(g.mutants) {
			t.Fatalf("unit %s root %s: the probe answered %d lines for %d mutants", g.unit, g.root, len(lines), len(g.mutants))
		}
		res.roots++
		for i, mu := range g.mutants {
			ans, err := parseLuauFuzzLine(lines[i])
			if err != nil {
				t.Fatalf("unit %s root %s: %v", g.unit, g.root, err)
			}
			o := luauFuzzEngine(g.m, g.def, mu.wire)
			if o.framingRefused {
				res.framingRefused++
			}
			res.mutants++
			if v := luauFuzzVerdict(ans, o); v != "" {
				res.divergences = append(res.divergences, fmt.Sprintf("seed %#016x unit %s root %s case %s: %s\n    mutant %x",
					mu.seed, g.unit, g.root, mu.caseName, v, mu.wire))
			}
		}
	}
	return res
}

// luauFuzzRun writes one chunk's inputs.luau and runs the probe under the
// pinned interpreter with the unit directory as the working directory.
func luauFuzzRun(lua string, g *luauFuzzGroup, c *luauFuzzChunk) ([]byte, error) {
	var b strings.Builder
	b.WriteString("return table.freeze({\n")
	fmt.Fprintf(&b, "\troot = %q,\n", g.root)
	b.WriteString("\tmodules = {")
	for i, m := range g.mods {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", m)
	}
	b.WriteString("},\n\twires = {\n")
	for _, mu := range c.mutants {
		b.WriteString("\t\tbuffer.fromstring(\"")
		for _, x := range mu.wire {
			fmt.Fprintf(&b, "\\x%02x", x)
		}
		b.WriteString("\"),\n")
	}
	b.WriteString("\t},\n})\n")
	if err := os.WriteFile(filepath.Join(g.dir, "inputs.luau"), []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	cmd := exec.Command(lua, "wirefuzz.luau")
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("luau wirefuzz.luau: %w\nstderr:\n%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// luauFuzzCost is what the slow half pays for these tests (internal/slowtest):
// the pinned Luau interpreter over every mutant, tens of CPU-seconds, most of
// it tabledemo wide_blob's 210 KB instance read 200 times.
const luauFuzzCost = "the pinned Luau interpreter (dist/luau-0.741, make dist-luau) over every mutant of the corpus"

// luauFuzzSummary is the run's one line.
func luauFuzzSummary(r luauFuzzResult) string {
	return fmt.Sprintf("Luau wire fuzz: %d mutants over %d roots (%d the framing refuses), %d divergences",
		r.mutants, r.roots, r.framingRefused, len(r.divergences))
}

// TestLuauWireFuzzAgreesWithTheGoEngine is the differential: every instance
// wire of the corpus, mutated luauFuzzMutantsPerInstance times, read by the
// Luau leg and by the compiler's engine, and the two must agree on the six
// counters, the verdict and the re-saved bytes of every mutant
// (docs/SPEC-TABLES.md §4.2).
func TestLuauWireFuzzAgreesWithTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauFuzzCost)
	res := runLuauFuzzDifferential(t, nil)
	t.Log(luauFuzzSummary(res))
	if len(res.divergences) > 0 {
		n := min(len(res.divergences), 5)
		t.Fatalf("%s; first %d:\n%s", luauFuzzSummary(res), n, strings.Join(res.divergences[:n], "\n"))
	}
}

// luauFuzzControlAnchor is the string field's frame check in the emitted load
// (runtime/schema_load.luau, read_field): a length past the body is malformed (§3).
// The "str" branch is matched exactly once.
const luauFuzzControlAnchor = `	elseif s == "str" then
		local n = r_leb(r)
		if r.bad or r.at + n > r.e then _report.malformed = true; return false end`

// TestLuauWireFuzzNegativeControl patches the emitted load in memory — one
// anchor, matched exactly once — so a string frame length past the body stops
// the body WITHOUT counting it malformed, and requires the differential to go
// red on exactly that.
func TestLuauWireFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauFuzzCost)
	sabotaged := strings.Replace(luauFuzzControlAnchor, "_report.malformed = true; return false", "return false", 1)
	edit := func(src []byte) []byte {
		if n := strings.Count(string(src), luauFuzzControlAnchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor appears %d times in schema_load.luau, want 1", n)
		}
		return []byte(strings.Replace(string(src), luauFuzzControlAnchor, sabotaged, 1))
	}
	clean := runLuauFuzzDifferential(t, nil)
	res := runLuauFuzzDifferential(t, edit)
	t.Logf("clean: %s", luauFuzzSummary(clean))
	t.Logf("sabotaged: %s", luauFuzzSummary(res))
	if len(res.divergences) <= len(clean.divergences) {
		t.Fatalf("NEGATIVE CONTROL FAILED: a load that does not count a string frame past the body left the differential at %d divergences (clean %d)",
			len(res.divergences), len(clean.divergences))
	}
	before := map[string]bool{}
	for _, d := range clean.divergences {
		before[d] = true
	}
	removed := regexp.MustCompile(`Luau says \{[^}]*malformed:false\}, the engine says \{[^}]*malformed:true\}`)
	for _, d := range res.divergences {
		if !before[d] && removed.MatchString(d) {
			return
		}
	}
	t.Fatal("NEGATIVE CONTROL FAILED: the differential went red, but no divergence is the malformed count the sabotage removed")
}
