package rusttable

// I13 (docs/PORTING.md, "I13 — The text differential against a third
// implementation") for the Rust leg, in the shape of the Java, Dart and Go
// cells: the leg emits random instances as (wire, text), the compiler's own
// engine (internal/tabletext and internal/tablepack, written from the spec and
// from neither backend) reads the same wire and writes its text, the two texts
// are byte-compared; then the engine packs the leg's text back and the wires
// are compared; then the leg's own from_json reads the ENGINE's text and its
// save is compared with the wire.
//
// The Rust probe is compiled with rustc and draws its instances from the same
// splitmix64 the other legs use, from a pinned seed: no real time, no sockets.
// Two root spellings run, as the Dart cell's two do:
//
//   - variable: a variable-form root over scalar float32/float64, a nested
//     table, []float32, []float64 and a list of tables;
//   - fixed: the Go cell's schema, a `fixed table` over a plain type with
//     bounded float arrays. The Rust fixed RECORD (RootRow, form byte 3) has no
//     text surface; what a fixed-class unit emits is the variable-form Root
//     beside it (form byte 1), whose to_json this spelling holds against the
//     engine. The text of a form-3 record is not carried in Rust and is not
//     claimed.
//
// Non-finite patterns are redrawn: JSON has no spelling for them and the writer
// refuses them (§16.3), which the probe also checks. Instance 0 is the
// all-default root.

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rustTextDifferentialVariableSchema = `package probe
table Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many []float32
 deep []float64
 rows []Sample
}
`

const rustTextDifferentialFixedSchema = `package probe
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

var rustTextDifferentialSpellings = []struct {
	name   string
	schema string
	rows   bool
}{
	{"variable", rustTextDifferentialVariableSchema, true},
	{"fixed", rustTextDifferentialFixedSchema, false},
}

const (
	// the Go, Dart and Java cells' seed and count, so every leg draws the same
	// float patterns in the same order.
	rustTextDifferentialSeed = 24845619678
	rustTextDifferentialN    = 200
)

// rustTextDifferentialSeeds is every seed the row runs; seed 1 is Dart's
// second line (a float64 tie its first writer broke away from even).
var rustTextDifferentialSeeds = []uint64{rustTextDifferentialSeed, 1}

// rustTextDifferentialProbe is the probe main. `write <dir>` draws N instances
// (seed and N from the environment) and writes each one's wire and text;
// `read <dir>` reads each engine-<i>.json through root_from_json_bytes and writes
// the saved wire as back-<i>.wire; `refuse` holds the checked writer to refusing
// a non-finite float (§16.3). ROWS_DRAW and ROWS_REFUSE are the list-of-tables
// lines the variable spelling carries.
const rustTextDifferentialProbe = `#![allow(dead_code, unused_imports)]
mod variable_runtime;
mod probe_table;

use probe_table::*;
use variable_runtime::*;
use std::fs;

struct SplitMix64(u64);

impl SplitMix64 {
    fn next(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9E3779B97F4A7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58476D1CE4E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D049BB133111EB);
        z ^ (z >> 31)
    }
    fn f32(&mut self) -> f32 {
        loop {
            let f = f32::from_bits(self.next() as u32);
            if f.is_finite() { return f; }
        }
    }
    fn f64(&mut self) -> f64 {
        loop {
            let f = f64::from_bits(self.next());
            if f.is_finite() { return f; }
        }
    }
}

fn fail(what: &str) -> ! {
    println!("PROBE FAILED: {what}");
    std::process::exit(1);
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let n: usize = std::env::var("SCHEMA_TEXT_DIFF_N").map(|v| v.parse().unwrap()).unwrap_or(0);
    match args[1].as_str() {
        "write" => {
            let dir = &args[2];
            let mut r = SplitMix64(std::env::var("SCHEMA_TEXT_DIFF_SEED").unwrap().parse().unwrap());
            for i in 0..n {
                let mut v = Root::default();
                if i != 0 {
                    v.a = r.f32();
                    v.b = r.f64();
                    v.pair.s = r.f32();
                    v.pair.d = r.f64();
                    for _ in 0..8 { v.many.push(r.f32()); }
                    for _ in 0..8 { v.deep.push(r.f64()); }
                    ROWS_DRAW
                }
                let wire = root_save(&v);
                let text = match root_to_json_checked(&v) {
                    Some(t) => t,
                    None => fail(&format!("instance {i}: to_json refused")),
                };
                fs::write(format!("{dir}/inst-{i}.wire"), &wire).unwrap();
                fs::write(format!("{dir}/inst-{i}.json"), text.as_bytes()).unwrap();
            }
            println!("probe wrote {n} instances");
        }
        "read" => {
            let dir = &args[2];
            for i in 0..n {
                let text = fs::read(format!("{dir}/engine-{i}.json")).unwrap();
                let mut rep = TableReport::default();
                let v = match root_from_json_bytes(&text, &mut rep) {
                    Some(v) => v,
                    None => fail(&format!("instance {i}: from_json of the engine text refused")),
                };
                if rep.malformed || rep.refused || rep.unknown != 0 || rep.kind_mismatch != 0 || rep.clamped != 0 || rep.duplicate != 0 {
                    fail(&format!("instance {i}: from_json of the engine text reports loss"));
                }
                fs::write(format!("{dir}/back-{i}.wire"), root_save(&v)).unwrap();
            }
            println!("probe read {n} instances");
        }
        "refuse" => {
            for f in [f32::NAN, f32::INFINITY, f32::NEG_INFINITY] {
                let mut v = Root::default();
                v.many.push(f);
                if root_to_json_checked(&v).is_some() { fail(&format!("to_json wrote a non-finite float32 {f}")); }
            }
            for d in [f64::NAN, f64::INFINITY, f64::NEG_INFINITY] {
                let mut v = Root::default();
                v.pair.d = d;
                if root_to_json_checked(&v).is_some() { fail(&format!("to_json wrote a non-finite float64 {d}")); }
            }
            println!("probe: to_json refused every non-finite float");
        }
        other => fail(&format!("unknown mode {other}")),
    }
}
`

const rustTextDifferentialRowsDraw = `for _ in 0..3 {
                        let mut s = Sample::default();
                        s.s = r.f32();
                        s.d = r.f64();
                        v.rows.push(s);
                    }`

// runRustTextDifferential generates the probe unit (with an optional in-memory
// edit of the emitted modules), builds it once with rustc beside the probe main,
// and for every seed runs the probe's write pass, holds every instance against
// the engine in both directions, then runs the read pass over the engine's
// texts. It returns the divergence messages and a non-nil error when any
// instance differs, so the control asserts on what the row itself prints.
func runRustTextDifferential(t *testing.T, schema string, rows bool, seeds []uint64, n int, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "rustc (it compiles and runs the generated text-differential probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, schema)
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(modules)
	}
	root := t.TempDir()
	for _, m := range []string{"variable_runtime", "probe_table"} {
		data, ok := modules[m+".rs"]
		if !ok {
			t.Fatalf("missing module %s.rs; got %v", m, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, m+".rs"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	draw := ""
	if rows {
		draw = rustTextDifferentialRowsDraw
	}
	main := strings.ReplaceAll(rustTextDifferentialProbe, "ROWS_DRAW", draw)
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}

	m := tabletext.NewModel(u)
	var msgs []string
	for _, seed := range seeds {
		dir := filepath.Join(root, fmt.Sprintf("inst-%d", seed))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		env := append(os.Environ(),
			"SCHEMA_TEXT_DIFF_SEED="+strconv.FormatUint(seed, 10),
			"SCHEMA_TEXT_DIFF_N="+strconv.Itoa(n))
		runProbe := func(mode string) ([]byte, error) {
			cmd := exec.Command(bin, mode, dir)
			cmd.Env = env
			return cmd.CombinedOutput()
		}
		if out, err := runProbe("write"); err != nil {
			return out, fmt.Errorf("probe write (seed=%d): %w", seed, err)
		}
		legMsgs, err := compareRustTextDifferential(m, dir, n)
		if err != nil {
			return []byte(strings.Join(legMsgs, "\n\n")), fmt.Errorf("seed=%d: %w", seed, err)
		}
		msgs = append(msgs, legMsgs...)
		if len(legMsgs) > 0 {
			continue
		}
		if out, err := runProbe("read"); err != nil {
			return out, fmt.Errorf("probe read (seed=%d): %w", seed, err)
		}
		for i := range n {
			wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
			if err != nil {
				return nil, err
			}
			back, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("back-%d.wire", i)))
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(back, wire) {
				msgs = append(msgs, fmt.Sprintf("seed %d instance %d: leg wire saved from from_json of the engine text differs from leg wire: back=%x leg=%x\nfloats: %s", seed, i, back, wire, rustFloatBitsOf(m, wire)))
			}
		}
	}
	if out, err := exec.Command(bin, "refuse", root).CombinedOutput(); err != nil {
		msgs = append(msgs, fmt.Sprintf("probe refuse: %v\n%s", err, out))
	}
	t.Logf("text differential (Rust): %d instances at each of %d seed(s), both directions and the engine text read back through from_json", n, len(seeds))
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d divergence(s) over %d instances x %d seeds", len(msgs), n, len(seeds))
	}
	return nil, nil
}

// TestTextDifferentialRust is the row: 200 pinned-seed random float instances
// at each of two seeds, of a variable root and of a fixed-class unit's root,
// held byte for byte against the engine in both directions and through the
// leg's from_json of the engine's text.
func TestTextDifferentialRust(t *testing.T) {
	t.Parallel()
	for _, sp := range rustTextDifferentialSpellings {
		t.Run(sp.name, func(t *testing.T) {
			t.Parallel()
			out, err := runRustTextDifferential(t, sp.schema, sp.rows, rustTextDifferentialSeeds, rustTextDifferentialN, nil)
			if err != nil {
				t.Fatalf("text differential (Rust, %s root): %v\n%s", sp.name, err, out)
			}
		})
	}
}

// rustTextDifferentialAnchor is the float window the emitted writer opens: six
// to nine significant digits for a float32, fifteen to seventeen for a
// float64. The control narrows the float32 window to six, so a float32 that
// needs seven, eight or nine renders short and only the cross-implementation
// comparison sees it.
const rustTextDifferentialAnchor = "let (low, high) = if single { (6, 9) } else { (15, 17) };"

// TestTextDifferentialRustNegativeControl caps the emitted float32 writer at
// six significant digits in memory. The gate must go red on the TEXT
// comparison, not on a compile error.
func TestTextDifferentialRustNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runRustTextDifferential(t, rustTextDifferentialVariableSchema, true, []uint64{rustTextDifferentialSeed}, rustTextDifferentialN, func(modules map[string][]byte) {
		data := string(modules["variable_runtime.rs"])
		if c := strings.Count(data, rustTextDifferentialAnchor); c != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times in variable_runtime.rs, want 1", rustTextDifferentialAnchor, c)
		}
		modules["variable_runtime.rs"] = []byte(strings.Replace(data, rustTextDifferentialAnchor, "let (low, high) = if single { (6, 6) } else { (15, 17) }; /* SABOTAGED */", 1))
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("negative control red on the text comparison, as it must be: %v", err)
}

// compareRustTextDifferential runs both engine directions for every instance:
// the engine reads the leg's wire and writes its text, byte-compared against
// the leg's text; the engine packs the leg's text, byte-compared against the
// leg's wire. It writes the engine's text as engine-<i>.json for the probe's
// read pass. Divergences are collected, not failed, so the control can assert
// on them.
func compareRustTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
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
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		os.RemoveAll(d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("engine-%d.json", i)), engineJSON, 0o600); err != nil {
			return msgs, err
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := rustTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, rustFloatBitsOf(m, wire)))
			continue
		}

		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			os.RemoveAll(d2)
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		os.RemoveAll(d2)
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

// rustFloatBitsOf decodes one wire with the engine and spells every float
// slot's bits, so a divergence replays from the bits alone.
func rustFloatBitsOf(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// rustTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func rustTextWindow(a, b []byte) (int, string) {
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
