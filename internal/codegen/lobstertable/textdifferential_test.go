package lobstertable_test

// I13 (docs/PORTING.md, "The text differential against a third
// implementation") on the Lobster leg. The leg writes N random float
// instances as (wire, text) through the emitted runtime's save and
// json_write; the compiler's own engine (internal/tabletext and
// internal/tablepack, written from the spec and from neither backend) reads
// the same wire and writes its text, and the two texts are byte-compared; then
// the engine packs the leg's text back and that wire is byte-compared with the
// leg's wire.
//
// The pinned corpus (TestLobsterJsonMatchesTheReference) never reaches a float
// tie, and a float tie is where text writers disagree, so the instances are
// random. The bit patterns are drawn here from a splitmix64 stream at the
// Elixir and Lua legs' seed and handed to the program as a file, which reads
// each through read_float32_le or read_float64_le into the value; a
// non-finite pattern (an all-ones exponent) has no JSON spelling and is
// redrawn. Instance 0 is the all-defaults value. The schema is the Elixir and
// Lua legs': two floats, a nested type of two, a bounded list of float32, an
// unbounded list of each width.
//
// It runs the pinned toolchain, so it is a functional test behind the slow
// gate: make tables-lobster-json-differential runs it through
// test/slowgate/proof.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const lobsterTextDifferentialSchema = `package textdiff
type Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 wide []float32
 deep []float64
}
`

const (
	lobsterTextDifferentialSeed = 24845619678
	lobsterTextDifferentialN    = 200
)

// lobsterTextDifferentialWidths is the order the program reads one
// instance's patterns in: a, b, pair.s, pair.d, many (8), wide (8), deep (8);
// true is a float32.
var lobsterTextDifferentialWidths = func() []bool {
	w := []bool{true, false, true, false}
	for range 8 {
		w = append(w, true)
	}
	for range 8 {
		w = append(w, true)
	}
	for range 8 {
		w = append(w, false)
	}
	return w
}()

// lobsterTextDifferentialBits is the patterns of instances 1..n-1, eight bytes
// each, a float32 in the low four: splitmix64, an all-ones exponent redrawn.
func lobsterTextDifferentialBits(seed uint64, n int) []byte {
	state := seed
	next := func() uint64 {
		state += 0x9E3779B97F4A7C15
		z := state
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
	var out []byte
	for i := 1; i < n; i++ {
		for _, single := range lobsterTextDifferentialWidths {
			var v uint64
			for {
				v = next()
				if single {
					v &= 0xFFFFFFFF
					if v&0x7F800000 != 0x7F800000 {
						break
					}
				} else if v&0x7FF0000000000000 != 0x7FF0000000000000 {
					break
				}
			}
			out = binary.LittleEndian.AppendUint64(out, v)
		}
	}
	return out
}

// lobsterTextDifferentialProgram builds each instance as the load's data
// model (a Record), saves it and writes its text.
const lobsterTextDifferentialProgram = `let bits = read_file("bits")
assert bits
var k = 0
def take(single:bool) -> float:
    let at = k * 8
    k++
    return if single: read_float32_le(bits, at) else: read_float64_le(bits, at)
let n = string_to_int(command_line_arguments()[0])
for(n) i:
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    if i != 0:
        rec.fields[0].cell.f = take(true)
        rec.fields[1].cell.f = take(false)
        let pair = rec.fields[2].cell.tab
        assert pair
        pair.fields[0].cell.f = take(true)
        pair.fields[1].cell.f = take(false)
        let many = rec.fields[3]
        many.count = 8
        for(8) j:
            many.elems[j].f = take(true)
        for(8):
            push(rec.fields[4].elems, schema_rt.Value { f: take(true) })
        for(8):
            push(rec.fields[5].elems, schema_rt.Value { f: take(false) })
    let wire = schema_rt.save(rec)
    let text, why = schema_rt.json_write(rec)
    assert why == ""
    assert write_file("inst-{i}.wire", wire)
    assert write_file("inst-{i}.json", text)
print("wrote {n}")
`

// runLobsterTextDifferential generates the probe unit, applies an optional
// edit to the emitted json.lobster, runs the program, then runs both
// directions of the engine comparison. It returns the divergence messages and
// a non-nil error when any instance differs, so the negative control asserts
// on the same output the row produces. A program that fails to run is fatal
// through lobsterrun's own diagnostics, never a divergence.
func runLobsterTextDifferential(t *testing.T, edit func(string) string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "textdiff.schema")
	if err := os.WriteFile(schema, []byte(lobsterTextDifferentialSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the probe unit is refused: %v", err)
	}
	if edit != nil {
		p := filepath.Join(g.dir, "json.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(edit(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\nimport .json\n")
	b.WriteString(lobsterTextDifferentialProgram)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	bits := lobsterTextDifferentialBits(lobsterTextDifferentialSeed, lobsterTextDifferentialN)
	out, code := lobsterrun.RunExit(t, g.dir, "main.lobster", map[string][]byte{"bits": bits}, fmt.Sprint(lobsterTextDifferentialN))
	if code != 0 || !strings.Contains(string(out), fmt.Sprintf("wrote %d", lobsterTextDifferentialN)) {
		t.Fatalf("the program failed (exit %d):\n%s", code, out)
	}
	m := tabletext.NewModel(g.u)
	msgs, err := compareLobsterTextDifferential(m, g.dir, lobsterTextDifferentialN)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d of %d instance(s) differ", len(msgs), lobsterTextDifferentialN)
	}
	return nil, nil
}

// TestLobsterTextDifferential is the row: the emitted Lobster text writer and
// the engine's agree, byte for byte, over 200 random float instances, in both
// directions.
func TestLobsterTextDifferential(t *testing.T) {
	t.Parallel()
	out, err := runLobsterTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("text differential: %v\n%s", err, out)
	}
	t.Logf("%d random float instances: the leg's text and wire agree with the engine both ways", lobsterTextDifferentialN)
}

// lobsterTextDifferentialAnchor is the float32 digit ceiling in json.lobster's
// j_float_text, the §16.7 shortest-round-trip loop over %.*g.
const lobsterTextDifferentialAnchor = "    let high = if single: 9 else: 17\n"

// TestLobsterTextDifferentialNegativeControl caps a float32's text at SIX
// significant digits, so a float32 that needs seven, eight or nine renders
// short and only the cross-implementation comparison sees it. The gate must
// go red on the text comparison, not on a program failure.
func TestLobsterTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runLobsterTextDifferential(t, func(src string) string {
		if c := strings.Count(src, lobsterTextDifferentialAnchor); c != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times in json.lobster, want 1", lobsterTextDifferentialAnchor, c)
		}
		return strings.Replace(src, lobsterTextDifferentialAnchor, "    let high = if single: 6 else: 17\n", 1)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("control red as required: %v", err)
}

// compareLobsterTextDifferential runs both directions for every instance the
// program wrote. Direction one: the engine reads the leg's wire and writes its
// text, byte-compared with the leg's text. Direction two: the engine packs the
// leg's text back to wire, byte-compared with the leg's wire.
func compareLobsterTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}
		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
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
			off := 0
			for off < len(engineJSON) && off < len(legJSON) && engineJSON[off] == legJSON[off] {
				off++
			}
			lo := max(off-32, 0)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\nleg[%d:]=%q\nengine[%d:]=%q",
				i, off, lo, legJSON[lo:min(off+32, len(legJSON))], lo, engineJSON[lo:min(off+32, len(engineJSON))]))
			continue
		}
		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
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
