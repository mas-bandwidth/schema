package darttable

// I13 (docs/PORTING.md) is the text differential against a third
// implementation, here for the Dart leg: a Dart probe builds random instances
// with the generated setters and writes each as (wire, text); the compiler's
// own engine (internal/tabletext, written from the spec and from neither
// backend) reads the same wire and writes its text, the two are byte-compared,
// then the engine packs the leg's text back to wire and that is byte-compared
// with the leg's wire. This is the port of the Go leg's cell
// (internal/codegen/gotable/textdifferential_test.go): the same splitmix64,
// the same seed and count, finite floats only (both writers refuse a
// non-finite one), instance 0 the reset proof.
//
// The root is a VARIABLE table (form 1): the Go cell's shape without `fixed`,
// because form 1 refuses a closure holding a fixed table until the fixed class
// rides the table wire. Only the instance generator is Dart; the comparison
// runs here, inside Go.
//
// Running the Dart SDK makes this a functional test: it skips by name when the
// SDK is absent, fails by name under SCHEMA_REQUIRE_CORPUS=1, and is reached
// from make tables-dart-text-differential.

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

const dartTextDifferentialSchema = `package probe
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

// dartTextDifferentialProbe builds N random float-only instances with the
// generated setters and the Go cell's splitmix64, then writes each instance's
// wire and text. Seed, count and directory arrive through the environment so
// the outer test owns them. Instance 0 is a fresh Root alone; instances 1..N-1
// fill every float slot, redrawing any non-finite 32- or 64-bit pattern, in
// the Go cell's draw order.
const dartTextDifferentialProbe = `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'ProbeWire.dart';

final class SplitMix64 {
  int state;
  SplitMix64(this.state);

  int next() {
    state += 0x9E3779B97F4A7C15;
    var z = state;
    z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9;
    z = (z ^ (z >>> 27)) * 0x94D049BB133111EB;
    return z ^ (z >>> 31);
  }
}

final ByteData _scratch = ByteData(8);

double nextFloat32(SplitMix64 r) {
  for (;;) {
    _scratch.setUint32(0, r.next() & 0xFFFFFFFF);
    final f = _scratch.getFloat32(0);
    if (!f.isNaN && !f.isInfinite) return f;
  }
}

double nextFloat64(SplitMix64 r) {
  for (;;) {
    _scratch.setInt64(0, r.next());
    final f = _scratch.getFloat64(0);
    if (!f.isNaN && !f.isInfinite) return f;
  }
}

void main() {
  final env = Platform.environment;
  final dir = env['SCHEMA_TEXT_DIFF_DIR'];
  if (dir == null || dir.isEmpty) {
    stderr.writeln('SCHEMA_TEXT_DIFF_DIR is unset');
    exit(1);
  }
  final seed = int.parse(env['SCHEMA_TEXT_DIFF_SEED'] ?? '');
  final n = int.parse(env['SCHEMA_TEXT_DIFF_N'] ?? '');
  final r = SplitMix64(seed);
  for (var i = 0; i < n; i++) {
    final v = Root();
    if (i != 0) {
      v.a = nextFloat32(r);
      v.b = nextFloat64(r);
      v.pair.s = nextFloat32(r);
      v.pair.d = nextFloat64(r);
      v.many = <double>[for (var j = 0; j < 8; j++) nextFloat32(r)];
      v.deep = <double>[for (var j = 0; j < 8; j++) nextFloat64(r)];
    }
    File('$dir/inst-$i.wire').writeAsBytesSync(rootWireSave(v));
    File('$dir/inst-$i.json').writeAsBytesSync(utf8.encode(rootToJson(v)));
  }
  stdout.writeln('ok $n');
}
`

const (
	// textDifferentialSeed and textDifferentialN are the Go cell's constants
	// (internal/codegen/gotable/textdifferential_test.go), so the two legs draw
	// the same float patterns in the same order.
	textDifferentialSeed = 24845619678
	textDifferentialN    = 200
)

// dartTextDifferentialSeeds is every seed the row runs, each a regression
// line: both exposed divergences on the first run, before textform.go's
// _formatG rounded an exact tie half to even (docs/SPEC-TABLES.md §16.2).
// At the Go cell's seed, 6 of 200 instances differed, every one a float32 tie
// toStringAsFixed broke away from even (instance 5, many[1]=0x4a5d3985:
// Dart 3624545.3, engine 3624545.2); at seed 1, 3 of 200 (instance 184, a
// float64 tie: Dart -1699035690989648.3, engine -1699035690989648.2).
var dartTextDifferentialSeeds = []uint64{
	textDifferentialSeed,
	1,
}

// runDartTextDifferential generates the Dart library for the probe unit,
// applies an optional in-memory edit to the emitted files, runs the probe at
// seed with n instances, and then the engine comparison. It returns the
// divergence messages and a non-nil error when any instance differs.
func runDartTextDifferential(t *testing.T, seed uint64, n int, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "dart")
	dart := dartBinary(t)
	u := buildUnit(t, dartTextDifferentialSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := files["ProbeWire.dart"]; !ok {
		t.Fatal("Generate emitted no ProbeWire.dart for the probe unit")
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(dartTextDifferentialProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SCHEMA_TEXT_DIFF_DIR="+out,
		"SCHEMA_TEXT_DIFF_SEED="+strconv.FormatUint(seed, 10),
		"SCHEMA_TEXT_DIFF_N="+strconv.Itoa(n),
	)
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		return probeOut, fmt.Errorf("dart probe (seed=%d n=%d): %w", seed, n, err)
	}
	m := tabletext.NewModel(u)
	msgs, err := compareDartTextDifferential(m, out, n)
	if err != nil {
		return probeOut, fmt.Errorf("seed=%d n=%d: %w", seed, n, err)
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("seed=%d n=%d: %d instance(s) differ", seed, n, len(msgs))
	}
	return probeOut, nil
}

// TestDartTextDifferentialAgainstTheGoEngine is the row: the generated Dart
// text writer and the engine's float writer (internal/tabletext/write.go,
// writeFloat/formatG, C's %.*g spelled out; docs/SPEC-TABLES.md §16.2) agree
// byte for byte over random float-only instances, in both directions, at
// every seed of dartTextDifferentialSeeds.
func TestDartTextDifferentialAgainstTheGoEngine(t *testing.T) {
	t.Parallel()
	for _, seed := range dartTextDifferentialSeeds {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			out, err := runDartTextDifferential(t, seed, textDifferentialN, nil)
			if err != nil {
				t.Fatalf("Dart text differential: %v\n%s", err, out)
			}
		})
	}
}

// dartTextDifferentialAnchor is the one line of the emitted float writer the
// negative control patches: the float32 precision ceiling.
const dartTextDifferentialAnchor = "final high = single ? 9 : 17;"

// TestDartTextDifferentialNegativeControl caps a float32's text at SIX
// significant digits in the emitted Dart (in memory), so a float32 that needs
// seven, eight or nine renders short and only the cross-implementation
// comparison sees it. The row must go red on the text comparison, not on a
// compile error or a probe failure.
func TestDartTextDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runDartTextDifferential(t, textDifferentialSeed, textDifferentialN, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += bytes.Count(data, []byte(dartTextDifferentialAnchor))
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", dartTextDifferentialAnchor, total)
		}
		for name, data := range files {
			files[name] = bytes.Replace(data, []byte(dartTextDifferentialAnchor), []byte("final high = single ? 6 : 17; // SABOTAGED"), 1)
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a Dart float32 writer capped at six significant digits left the row GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the row went red, but not on the text comparison: %v\n%s", err, out)
	}
}

// compareDartTextDifferential runs both directions of the engine comparison
// for every instance the probe wrote. Direction one: the engine reads the leg's
// wire and writes its text, byte-compared against the leg's text. Direction
// two: the engine packs the leg's text back to wire, byte-compared against the
// leg's wire. Divergences are collected as messages, not failures, so the
// negative control can assert on them.
func compareDartTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
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
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := dartTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, dartFloatBitsOf(m, wire)))
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

// dartFloatBitsOf decodes one wire with the engine and spells every float
// field's bits, so a divergence replays from the bits alone.
func dartFloatBitsOf(m *tabletext.Model, wire []byte) string {
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

// dartTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func dartTextWindow(a, b []byte) (int, string) {
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
