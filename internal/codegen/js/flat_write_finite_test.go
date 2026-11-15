package js

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJsFlatWriteRefusesNonFiniteFloats pins SPEC §5.1 row 7 on the JavaScript
// flat tier: a bare (unquantized) float32 whose float32 rounding is not finite
// and a bare float64 that is not finite are refused with -1. The pure half
// inspects the emitted writer for the finite guard on both fields, alone and
// beside a uint8; the gated half writes NaN, the infinities and a finite
// double that rounds to an infinite float32 (expecting refusal) and finite,
// -0, subnormal and top-of-range values (expecting a bit-exact round trip).
func TestJsFlatWriteRefusesNonFiniteFloats(t *testing.T) {
	t.Parallel()

	flat := generateWriteFiniteProbeFlat(t)
	for _, name := range []string{"WriteProbeFlat", "WritePackedFlat"} {
		write := emittedFunction(t, flat, name)
		for _, want := range []string{
			"if (!Number.isFinite(Math.fround(x))) {",
			"if (!Number.isFinite(value.f64)) {",
			"return -1;",
		} {
			if !strings.Contains(write, want) {
				t.Errorf("%s must refuse non-finite bare floats; missing %q", name, want)
			}
		}
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node (it compiles and runs the generated flat codec)")
		node := jsFlatNode(t)
		for _, production := range []bool{false, true} {
			label := "checked"
			if production {
				label = "production"
			}
			t.Run(label, func(t *testing.T) {
				runWriteFiniteProbe(t, node, flat, production)
			})
		}
	})
}

// generateWriteFiniteProbeFlat compiles a type carrying one bare float32 and
// one bare float64, alone and in a packed run beside a uint8.
func generateWriteFiniteProbeFlat(t *testing.T) string {
	t.Helper()
	src := `package clif

type Probe
{
    f32 float32
    f64 float64
}

type Packed
{
    k uint8
    f32 float32
    f64 float64
}
`
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	flat, ok := out["ProbeFlat.js"]
	if !ok {
		t.Fatalf("no flat module: %v", keys(out))
	}
	return string(flat)
}

// runWriteFiniteProbe writes the generated flat module beside a harness that
// refuses each non-finite value and round-trips the finite ones, then runs it.
func runWriteFiniteProbe(t *testing.T, node, flat string, production bool) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeFlat.js"), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import { WriteProbeFlat, ReadProbeFlat } from './ProbeFlat.js';

const buf = new ArrayBuffer(64);
const view = new DataView(buf);

function f32Bits(x) {
  const dv = new DataView(new ArrayBuffer(4));
  dv.setFloat32(0, x, true);
  return dv.getUint32(0, true);
}
function f64Bits(x) {
  const dv = new DataView(new ArrayBuffer(8));
  dv.setFloat64(0, x, true);
  return [dv.getUint32(0, true), dv.getUint32(4, true)];
}

function expectRefused(f32, f64, label) {
  const n = WriteProbeFlat(view, { f32: f32, f64: f64 });
  if (n !== -1) {
    throw new Error('flat writer returned ' + n + ' for ' + label + ', want -1');
  }
}

expectRefused(NaN, 1, 'NaN float32');
expectRefused(Infinity, 1, '+Infinity float32');
expectRefused(-Infinity, 1, '-Infinity float32');
expectRefused(1e39, 1, '1e39 float32');
expectRefused(1, NaN, 'NaN float64');
expectRefused(1, Infinity, '+Infinity float64');
expectRefused(1, -Infinity, '-Infinity float64');

function checkBitExact(f32, f64, label) {
  const n = WriteProbeFlat(view, { f32: f32, f64: f64 });
  if (n < 0) {
    throw new Error('flat writer refused finite ' + label);
  }
  const out = { f32: 0, f64: 0 };
  if (!ReadProbeFlat(view, n * 8, out)) {
    throw new Error('flat reader refused finite ' + label);
  }
  const got32 = f32Bits(out.f32);
  if (got32 !== f32Bits(f32)) {
    throw new Error(label + ' float32 bits changed: ' + got32.toString(16) + ' != ' + f32Bits(f32).toString(16));
  }
  const [lo, hi] = f64Bits(out.f64);
  const [wantLo, wantHi] = f64Bits(f64);
  if (lo !== wantLo || hi !== wantHi) {
    throw new Error(label + ' float64 bits changed: ' + lo.toString(16) + ',' + hi.toString(16) +
      ' != ' + wantLo.toString(16) + ',' + wantHi.toString(16));
  }
}

checkBitExact(1.5, 1.5, 'finite');
checkBitExact(-0, -0, 'negative zero');
checkBitExact(1.401298464324817e-45, 5e-324, 'smallest subnormal');
checkBitExact(3.4028234663852886e38, 1.7976931348623157e308, 'largest finite');

console.log('OK');
`
	if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	env := os.Environ()
	if production {
		env = append(env, "NODE_ENV=production")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("probe output: %s", out)
	}
}
