package goosetable

// I13 (docs/PORTING.md), the text differential against a third
// implementation, for the Goose leg: the leg draws N random instances and
// writes each as text, and prints every float's BITS beside it; the
// compiler's own engine (internal/tabletext, internal/tablewire and
// internal/tablepack — `schema pack` and `schema unpack`, written from the
// spec and from neither backend) builds the same instance from those bits and
// writes ITS text, and the two texts are byte-compared. Then the other
// direction: the engine reads the leg's text (`schema pack`) and must land the
// wire of the instance the bits name. A pinned corpus never reaches a float
// tie, so the random source is a splitmix64 written into the probe, redrawing
// any non-finite pattern (a non-finite float has no JSON spelling and both
// writers refuse it); instance 0 is the record at its defaults.
//
// The bits stand where the C# and Go legs hand the engine the leg's WIRE: the
// Goose table wire is card schema-goose-table-wire-save's, and until it lands
// the bits are the value's identity, exactly and with no float formatting on
// the way.
//
// The unit is the Go and C# legs' float-only unit, byte for byte. The control
// caps the walk's float32 precision at six significant digits, so a float32
// that needs seven, eight or nine renders short and only the comparison sees
// it (make tables-goose-json-differential-negative-control).

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const gooseTextDifferentialSchema = `package probe
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

const (
	gooseTextDifferentialSeed = 24845619678
	gooseTextDifferentialN    = 200

	// the walk's float32 precision ceiling (runtime/schema_json.goose,
	// json_put_float); the negative control caps it at six
	gooseTextDifferentialAnchor   = "        high = 9;\n"
	gooseTextDifferentialSabotage = "        high = 6; // SABOTAGED\n"
)

const gooseTextDifferentialProbe = `import os;
%s
var state: u64 = %d;

fn next() -> u64 {
    state += 0x9E3779B97F4A7C15;
    var z = state;
    z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9;
    z = (z ^ (z >> 27)) * 0x94D049BB133111EB;
    return z ^ (z >> 31);
}

fn next32() -> f32 {
    loop {
        let f = schema_rt::fixed_bits_f32(next() as! u32);
        if f - f == 0.0 { return f; }
    }
}

fn next64() -> f64 {
    loop {
        let f = schema_rt::fixed_bits_f64(next());
        if f - f == 0.0 { return f; }
    }
}

fn bits32(f: f32) -> u64 { return schema_rt::fixed_f32_bits(f); }
fn bits64(f: f64) -> u64 { return schema_rt::fixed_f64_bits(f); }

fn main() {
    var i: i64 = 0;
    while i < %d {
        var v = probe::NewRoot();
        if i != 0 {
            v.A = next32();
            v.B = next64();
            v.Pair.S = next32();
            v.Pair.D = next64();
            v.ManyCount = 8;
            var j: i64 = 0;
            while j < 8 {
                v.Many[j] = next32();
                j += 1;
            }
            v.DeepCount = 8;
            j = 0;
            while j < 8 {
                v.Deep[j] = next64();
                j += 1;
            }
        }
        print(bits32(v.A), " ", bits64(v.B), " ", bits32(v.Pair.S), " ", bits64(v.Pair.D));
        var j2: i64 = 0;
        while j2 < 8 {
            print(bits32(v.Many[j2]), " ", bits64(v.Deep[j2]));
            j2 += 1;
        }
        var text: u8[>..] = [];
        let n = probe::RootToJson(v, text);
        if n < 0 || n != probe::RootToJsonMeasure(v) {
            print("PROBE FAILED: instance ", i, ": ToJson ", n);
            return;
        }
        var name: u8[>..] = [];
        format(name, "inst-", i, ".json");
        if !write_file(name, text) {
            print("PROBE FAILED: write ", i);
            return;
        }
        i += 1;
    }
    print("PROBE wrote ", %d);
}
`

// runGooseTextDifferential generates the unit, lets edit change the emitted
// files, runs the probe and answers every divergence from the engine.
func runGooseTextDifferential(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(gooseTextDifferentialSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, "import ."+before+";")
		}
	}
	sort.Strings(mods)
	main := fmt.Sprintf(gooseTextDifferentialProbe, strings.Join(mods, "\n"), uint64(gooseTextDifferentialSeed), gooseTextDifferentialN, gooseTextDifferentialN)
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := gooserun.Run(t, dir, "main.goose", nil)
	if !bytes.Contains(out, []byte(fmt.Sprintf("PROBE wrote %d", gooseTextDifferentialN))) {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
	m := tabletext.NewModel(u)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 9*gooseTextDifferentialN+1 {
		t.Fatalf("the probe printed %d lines, want %d", len(lines), 9*gooseTextDifferentialN+1)
	}
	var msgs []string
	for i := range gooseTextDifferentialN {
		inst, err := gooseTextDiffInstance(m, lines[9*i:9*i+9], i == 0)
		if err != nil {
			t.Fatal(err)
		}
		want, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("instance %d: the engine's encode: %v", i, err)
		}
		leg, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			t.Fatal(err)
		}
		d1 := t.TempDir()
		if err := os.WriteFile(filepath.Join(d1, "Root.json"), leg, 0o644); err != nil {
			t.Fatal(err)
		}
		wire, _, report, err := tablepack.Pack(m, "Root", d1)
		if err != nil || report != (tabletext.Report{}) {
			msgs = append(msgs, fmt.Sprintf("instance %d: the engine does not read the leg's text cleanly: %v %+v", i, err, report))
		} else if !bytes.Equal(wire, want) {
			msgs = append(msgs, fmt.Sprintf("instance %d: the engine reads the leg's text to another value (leg text and engine text differ in value)", i))
		}
		d2 := t.TempDir()
		if report, err := tablepack.UnpackOneFile(m, "Root", want, d2); err != nil || report != (tabletext.Report{}) {
			t.Fatalf("instance %d: the engine's unpack: %v %+v", i, err, report)
		}
		engine, err := os.ReadFile(filepath.Join(d2, "Root.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(engine, leg) {
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ %s", i, firstDiff(leg, engine)))
		}
	}
	return msgs
}

// gooseTextDiffInstance builds the engine's instance from one probe's nine
// lines of bits, in the draw order.
func gooseTextDiffInstance(m *tabletext.Model, lines []string, defaults bool) (*tabletext.Instance, error) {
	var bits []uint64
	for _, l := range lines {
		for w := range strings.FieldsSeq(l) {
			v, err := strconv.ParseUint(w, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("probe line %q: %v", l, err)
			}
			bits = append(bits, v)
		}
	}
	if len(bits) != 20 {
		return nil, fmt.Errorf("probe lines %q: %d values, want 20", lines, len(bits))
	}
	f32 := func(b uint64) float64 { return float64(math.Float32frombits(uint32(b))) }
	inst := m.New(m.Lookup("Root"))
	byName := map[string]*tabletext.Field{}
	for i := range inst.Fields {
		byName[inst.Fields[i].Def.Name] = &inst.Fields[i]
	}
	byName["a"].Cell.F = f32(bits[0])
	byName["b"].Cell.F = math.Float64frombits(bits[1])
	pair := byName["pair"].Cell.Tab
	pair.Fields[0].Cell.F = f32(bits[2])
	pair.Fields[1].Cell.F = math.Float64frombits(bits[3])
	many, deep := byName["many"], byName["deep"]
	for j := range 8 {
		many.Elems[j].F = f32(bits[4+2*j])
		deep.Elems[j].F = math.Float64frombits(bits[5+2*j])
	}
	if !defaults {
		many.Count, deep.Count = 8, 8
	}
	return inst, nil
}

func TestGooseTextDifferential(t *testing.T) {
	msgs := runGooseTextDifferential(t, nil)
	for _, m := range msgs {
		t.Error(m)
	}
	t.Logf("goose text differential: %d random instances, leg text == engine text", gooseTextDifferentialN)
}

func TestGooseTextDifferentialNegativeControl(t *testing.T) {
	msgs := runGooseTextDifferential(t, func(files map[string][]byte) {
		walk := files["schema_json.goose"]
		if n := bytes.Count(walk, []byte(gooseTextDifferentialAnchor)); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor %q appears %d times in schema_json.goose, want 1", gooseTextDifferentialAnchor, n)
		}
		files["schema_json.goose"] = bytes.Replace(walk, []byte(gooseTextDifferentialAnchor), []byte(gooseTextDifferentialSabotage), 1)
	})
	if len(msgs) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the differential GREEN")
	}
	for _, m := range msgs {
		if !strings.Contains(m, "leg text and engine text differ") {
			t.Fatalf("NEGATIVE CONTROL FAILED: red, but not on the text comparison: %s", m)
		}
	}
	red := map[string]bool{}
	for _, m := range msgs {
		inst, _, _ := strings.Cut(m, ":")
		red[inst] = true
	}
	t.Logf("goose text differential negative control: %d of %d instances red on the text comparison", len(red), gooseTextDifferentialN)
}
