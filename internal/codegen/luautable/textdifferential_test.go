package luautable_test

// I13 (docs/PORTING.md) is the text differential against a third
// implementation, here for the Luau leg: a Luau probe builds random
// float-only instances in the generated storage and writes each as (wire,
// text) through the emitted save and ToJson; the compiler's own engine
// (internal/tabletext, written from the spec and from neither backend) reads
// the same wire and writes its text, the two are byte-compared, then the
// engine packs the leg's text back to wire and that is byte-compared with the
// leg's wire. This is the port of the Go cell
// (internal/codegen/gotable/textdifferential_test.go) and the Dart one: the
// same splitmix64, the same seeds and count, finite floats only (both
// writers refuse a non-finite one), instance 0 the reset proof, and the same
// two spellings of the root (a VARIABLE table, and a `fixed table` over a
// plain `type`).
//
// The pinned Luau has no environment and no files, so the draws are made
// here, in the Go cell's order, and handed to the probe as one buffer of float
// bit patterns; the probe places them with the generated constructors and
// writes with the emitted runtime, so everything under test is Luau. Running
// the interpreter makes this a functional test: it sits behind slowtest.Gate
// and is reached from make tables-luau-text-differential.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const luauTextDifferentialSchema = `package probe
table Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

// luauTextDifferentialFixedSchema is the Go cell's spelling: the same fields,
// the root a fixed table over a plain type.
const luauTextDifferentialFixedSchema = `package probe
type Sample {
 s float32
 d float64
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

var luauTextDifferentialSpellings = []struct {
	name   string
	schema string
}{
	{"variable", luauTextDifferentialSchema},
	{"fixed", luauTextDifferentialFixedSchema},
}

const (
	// the Go cell's constants (internal/codegen/gotable/textdifferential_test.go),
	// so every leg draws the same float patterns in the same order.
	luauTextDifferentialSeed = 24845619678
	luauTextDifferentialN    = 200
	// one instance's draws: a, b, pair.s, pair.d, many[8], deep[8].
	luauTextDifferentialStride = 4 + 8 + 4 + 8 + 8*4 + 8*8
)

// luauTextDifferentialSeeds is every seed the row runs: the Go cell's, and
// seed 1, the Dart cell's float64-tie regression line.
var luauTextDifferentialSeeds = []uint64{luauTextDifferentialSeed, 1}

type luauSplitMix64 struct{ state uint64 }

func (r *luauSplitMix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *luauSplitMix64) float32Bits() uint32 {
	for {
		b := uint32(r.next())
		f := float64(math.Float32frombits(b))
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return b
		}
	}
}

func (r *luauSplitMix64) float64Bits() uint64 {
	for {
		b := r.next()
		f := math.Float64frombits(b)
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return b
		}
	}
}

// luauTextDifferentialDraws lays out instances 1..n-1's draws, in the Go
// cell's order, little-endian, one stride each.
func luauTextDifferentialDraws(seed uint64, n int) []byte {
	r := &luauSplitMix64{state: seed}
	out := make([]byte, 0, (n-1)*luauTextDifferentialStride)
	f32 := func() { out = binary.LittleEndian.AppendUint32(out, r.float32Bits()) }
	f64 := func() { out = binary.LittleEndian.AppendUint64(out, r.float64Bits()) }
	for i := 1; i < n; i++ {
		f32()
		f64()
		f32()
		f64()
		for range 8 {
			f32()
		}
		for range 8 {
			f64()
		}
	}
	return out
}

// luauTextDifferentialProbe builds every instance in the generated storage
// and prints "<wire hex> <text hex>" per instance.
func luauTextDifferentialProbe(g *generatedUnit, n int) string {
	return jsonModules(g) + fmt.Sprintf(`local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local draws: buffer = inputs.draws
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local desc = St.lookup("Root")
for i = 0, %d - 1 do
	local v: any = desc.new()
	if i ~= 0 then
		local at = (i - 1) * %d
		local function f32(): number local x = buffer.readf32(draws, at); at += 4; return x end
		local function f64(): number local x = buffer.readf64(draws, at); at += 8; return x end
		v.A = f32()
		v.B = f64()
		v.Pair.S = f32()
		v.Pair.D = f64()
		for j = 1, 8 do v.Many[j] = f32() end
		v.ManyCount = 8
		for j = 1, 8 do v.Deep[j] = f64() end
		v.DeepCount = 8
	end
	print(hex(save.save(desc, v)) .. " " .. hex(json.ToJson("Root", v)))
end
`, n, luauTextDifferentialStride)
}

// runLuauTextDifferential generates the Luau modules for the probe unit,
// passes the emitted JSON runtime through patch, runs the probe at seed with
// n instances, then the engine comparison. It returns the divergence messages
// and a non-nil error when any instance differs.
func runLuauTextDifferential(t *testing.T, schema string, seed uint64, n int, patch func(string) string) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe schema does not load: %v", err)
	}
	g := requiredFiles(t, u)
	if src := string(g.files["schema_json.luau"]); src == "" {
		t.Fatal("the probe unit emitted no schema_json.luau")
	} else if patch != nil {
		if err := os.WriteFile(filepath.Join(g.dir, "schema_json.luau"), []byte(patch(src)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauTextDifferentialProbe(g, n)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"draws": luauTextDifferentialDraws(seed, n)})
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != n {
		return string(out), fmt.Errorf("seed=%d n=%d: the probe printed %d lines", seed, n, len(lines))
	}
	// instance 0 is the fresh root; every drawn instance must say something else
	for i := 1; i < n; i++ {
		if lines[i] == lines[0] {
			return string(out), fmt.Errorf("seed=%d: instance %d wrote the fresh root, so the draws were not placed", seed, i)
		}
	}
	m := tabletext.NewModel(u)
	msgs, err := compareLuauTextDifferential(t, m, lines)
	if err != nil {
		return "", fmt.Errorf("seed=%d n=%d: %w", seed, n, err)
	}
	if len(msgs) > 0 {
		return strings.Join(msgs, "\n\n"), fmt.Errorf("seed=%d n=%d: %d instance(s) differ", seed, n, len(msgs))
	}
	return "", nil
}

// compareLuauTextDifferential runs both directions of the engine comparison
// for every instance the probe printed. Direction one: the engine reads the
// leg's wire and writes its text, byte-compared against the leg's text.
// Direction two: the engine packs the leg's text back to wire, byte-compared
// against the leg's wire. Divergences are messages, not failures, so the
// negative control can assert on them.
func compareLuauTextDifferential(t *testing.T, m *tabletext.Model, lines []string) ([]string, error) {
	t.Helper()
	var msgs []string
	for i, line := range lines {
		wireHex, textHex, ok := strings.Cut(line, " ")
		if !ok || wireHex == "nil" || textHex == "nil" {
			return msgs, fmt.Errorf("instance %d: the leg refused to write: %q", i, line)
		}
		wire, err := hex.DecodeString(wireHex)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: wire hex: %w", i, err)
		}
		legJSON, err := hex.DecodeString(textHex)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: text hex: %w", i, err)
		}
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := luauTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s", i, off, win))
			continue
		}
		d2 := t.TempDir()
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// luauTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func luauTextWindow(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}

// TestLuauTextDifferentialAgainstTheGoEngine is the row: the emitted Luau
// text writer and the engine's float writer (internal/tabletext/write.go,
// C's %.*g spelled out; docs/SPEC-TABLES.md §16.2) agree byte for byte over
// random float-only instances, in both directions, at every seed and every
// root spelling.
func TestLuauTextDifferentialAgainstTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	for _, sp := range luauTextDifferentialSpellings {
		for _, seed := range luauTextDifferentialSeeds {
			t.Run(fmt.Sprintf("%s/seed=%d", sp.name, seed), func(t *testing.T) {
				t.Parallel()
				if out, err := runLuauTextDifferential(t, sp.schema, seed, luauTextDifferentialN, nil); err != nil {
					t.Fatalf("Luau text differential (%s root): %v\n%s", sp.name, err, out)
				}
			})
		}
	}
}

// luauTextDifferentialAnchor is the one line of the emitted float writer the
// control patches (the anchor TestLuauJsonNegativeControl patches too).
const luauTextDifferentialAnchor = "local text = format_g(v, digits)"

// TestLuauTextDifferentialNegativeControl writes a float32 at six significant
// digits (C's %#.6g) in a scratch copy of the emitted runtime, so a float32
// needing more renders wrong and only the cross-implementation comparison
// sees it. The row must go red on the text comparison, not on a probe failure.
func TestLuauTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	out, err := runLuauTextDifferential(t, luauTextDifferentialSchema, luauTextDifferentialSeed, luauTextDifferentialN, func(s string) string {
		if strings.Count(s, luauTextDifferentialAnchor) != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor %q is not in the runtime exactly once", luauTextDifferentialAnchor)
		}
		return strings.Replace(s, luauTextDifferentialAnchor, `local text = single and string.format("%#.6g", v) or format_g(v, digits)`, 1)
	})
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: a Luau float32 writer capped at six significant digits left the row GREEN")
	}
	if !strings.Contains(out, "leg text and engine text differ") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the row went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("red on the text comparison: %v", err)
}
