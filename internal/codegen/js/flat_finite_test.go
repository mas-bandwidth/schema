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

// TestJsFlatRefusesNonFiniteBareFloats pins SPEC §4.3's float rows: a bare
// (unquantized) float32 or float64 is a finite IEEE-754 pattern, and a reader
// must refuse a NaN or an infinity on its raw bits as non_finite_float. The
// pure half inspects the generated reader text for the raw-bit refusal in both
// the float32 case and the float64 byte-aligned (static-cursor) case; the
// gated half runs the generated flat reader against buffers holding NaN and
// infinity patterns (expecting refusal) and finite, -0 and subnormal patterns
// (expecting bit-exact acceptance).
func TestJsFlatRefusesNonFiniteBareFloats(t *testing.T) {
	t.Parallel()

	flat := generateFiniteProbeFlat(t)
	read := emittedFunction(t, flat, "ReadProbeFlat")
	for _, want := range []string{
		"if ((v & 0x7f800000) === 0x7f800000)",
		"if ((view.getUint32(8, true) & 0x7ff00000) === 0x7ff00000)",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("flat reader must refuse non-finite bare floats on raw bits; missing %q", want)
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
				runFiniteProbe(t, node, production)
			})
		}
	})
}

// generateFiniteProbeFlat compiles a type carrying one bare float32 and one
// bare float64.
func generateFiniteProbeFlat(t *testing.T) string {
	t.Helper()
	src := `package clif

type Probe
{
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

// runFiniteProbe writes the generated flat module beside a harness that reads
// buffers holding NaN and infinity float32/float64 patterns (expecting the
// reader to refuse) and finite, -0 and subnormal patterns (expecting
// bit-exact acceptance), then runs it.
func runFiniteProbe(t *testing.T, node string, production bool) {
	t.Helper()
	dir := t.TempDir()
	flat := generateFiniteProbeFlat(t)
	if err := os.WriteFile(filepath.Join(dir, "ProbeFlat.js"), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import { ReadProbeFlat } from './ProbeFlat.js';

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

function expectRefused(f32bits, f64lo, f64hi, label) {
  view.setUint32(0, f32bits, true);
  view.setUint32(4, f64lo, true);
  view.setUint32(8, f64hi, true);
  const out = { f32: 0, f64: 0 };
  if (ReadProbeFlat(view, 96, out)) {
    throw new Error('flat read accepted ' + label);
  }
}

expectRefused(0x7fc00000, 0, 0x3ff00000, 'NaN float32');
expectRefused(0x7f800000, 0, 0x3ff00000, '+Infinity float32');
expectRefused(0xff800000, 0, 0x3ff00000, '-Infinity float32');
expectRefused(0x3f800000, 0, 0x7ff80000, 'NaN float64');
expectRefused(0x3f800000, 0, 0x7ff00000, '+Infinity float64');
expectRefused(0x3f800000, 0, 0xfff00000, '-Infinity float64');

function checkBitExact(f32bits, f64lo, f64hi, label) {
  view.setUint32(0, f32bits, true);
  view.setUint32(4, f64lo, true);
  view.setUint32(8, f64hi, true);
  const out = { f32: 0, f64: 0 };
  if (!ReadProbeFlat(view, 96, out)) {
    throw new Error('flat read refused finite ' + label);
  }
  const got32 = f32Bits(out.f32);
  if (got32 !== f32bits) {
    throw new Error(label + ' float32 bits changed: ' + got32.toString(16) + ' != ' + f32bits.toString(16));
  }
  const [lo, hi] = f64Bits(out.f64);
  if (lo !== f64lo || hi !== f64hi) {
    throw new Error(label + ' float64 bits changed: ' + lo.toString(16) + ',' + hi.toString(16) +
      ' != ' + f64lo.toString(16) + ',' + f64hi.toString(16));
  }
}

checkBitExact(0x3fc00000, 0, 0x3ff00000, 'finite');
checkBitExact(0xbfc00000, 0, 0xbff00000, 'negative finite');
checkBitExact(0x80000000, 0, 0x80000000, 'negative zero');
checkBitExact(0x00000001, 0, 0x00000001, 'smallest subnormal');
checkBitExact(0x007fffff, 0xffffffff, 0x000fffff, 'largest subnormal');

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
